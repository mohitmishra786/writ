// Package channel3 is a minimal client for the Channel3 product API
// (https://docs.trychannel3.com). Endpoint shapes follow the Channel3 API
// skill's reference. Following its guidance, only product IDs and category
// slugs are stable; prices are refetched rather than cached, and category
// slugs are discovered with categories/search rather than guessed.
package channel3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

const defaultBase = "https://api.trychannel3.com"

type Client struct {
	base string
	key  string
	http *http.Client
}

func New(apiKey string) *Client {
	return &Client{base: defaultBase, key: apiKey, http: &http.Client{Timeout: 20 * time.Second}}
}

func FromEnv() (*Client, error) {
	k := os.Getenv("CHANNEL3_API_KEY")
	if k == "" {
		return nil, errors.New("channel3: CHANNEL3_API_KEY is required")
	}
	return New(k), nil
}

type Price struct {
	Price          float64  `json:"price"`
	CompareAtPrice *float64 `json:"compare_at_price"`
	Currency       string   `json:"currency"`
}

type Offer struct {
	URL          string `json:"url"`
	Domain       string `json:"domain"`
	Price        Price  `json:"price"`
	Availability string `json:"availability"`
}

type Category struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type Product struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Category *Category `json:"category"`
	Images   []struct {
		URL string `json:"url"`
	} `json:"images"`
	Offers []Offer `json:"offers"`
}

type SearchRequest struct {
	Query        string
	CategorySlug string
	MaxPrice     float64
	Limit        int // Channel3 max is 30
}

func (c *Client) Search(ctx context.Context, r SearchRequest) ([]Product, error) {
	filters := map[string]any{"availability": []string{"InStock"}, "conditions": []string{"new"}}
	if r.CategorySlug != "" {
		filters["category_ids"] = []string{r.CategorySlug}
	}
	if r.MaxPrice > 0 {
		filters["price"] = map[string]any{"max_price": r.MaxPrice}
	}
	limit := r.Limit
	switch {
	case limit <= 0:
		limit = 20
	case limit > 30:
		limit = 30
	}
	body := map[string]any{"query": r.Query, "limit": limit, "filters": filters, "config": map[string]any{"currency": "USD", "country": "US"}}
	var out struct {
		Products []Product `json:"products"`
	}
	return out.Products, c.do(ctx, http.MethodPost, "/v1/search", body, &out)
}

// Product refetches current prices; Channel3 documents this call as free.
func (c *Client) Product(ctx context.Context, id string) (Product, error) {
	var p Product
	return p, c.do(ctx, http.MethodGet, "/v1/products/"+url.PathEscape(id), nil, &p)
}

func (c *Client) SearchCategories(ctx context.Context, query string) ([]Category, error) {
	var out struct {
		Categories []Category `json:"categories"`
	}
	q := url.Values{"query": {query}, "limit": {"5"}}
	return out.Categories, c.do(ctx, http.MethodGet, "/v1/categories/search?"+q.Encode(), nil, &out)
}

type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string { return fmt.Sprintf("channel3: %d: %s", e.Status, e.Detail) }

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Detail any `json:"detail"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: resp.StatusCode, Detail: fmt.Sprint(e.Detail)}
	}
	return json.Unmarshal(raw, out)
}

// Benchmark summarises live in-stock USD offer prices in integer cents. It is
// the market anchor the procurement agent negotiates against.
type Benchmark struct {
	Category string        `json:"category"`
	Offers   int           `json:"offers"`
	Min      money.Money   `json:"min"`
	P25      money.Money   `json:"p25"`
	Median   money.Money   `json:"median"`
	Max      money.Money   `json:"max"`
	Samples  []SampleOffer `json:"samples"`
}

type SampleOffer struct {
	ProductID string      `json:"product_id"`
	Title     string      `json:"title"`
	Domain    string      `json:"domain"`
	Price     money.Money `json:"price"`
}

var ErrNoOffers = errors.New("channel3: no in-stock USD offers to benchmark")

func BenchmarkOf(category string, products []Product) (Benchmark, error) {
	var samples []SampleOffer
	for _, p := range products {
		if category != "" && (p.Category == nil || p.Category.Slug != category) {
			continue
		}
		for _, o := range p.Offers {
			if o.Availability != "InStock" || o.Price.Currency != "USD" || o.Price.Price <= 0 {
				continue
			}
			m, err := money.FromFloat(o.Price.Price, money.USD)
			if err != nil {
				continue
			}
			samples = append(samples, SampleOffer{ProductID: p.ID, Title: p.Title, Domain: o.Domain, Price: m})
		}
	}
	if len(samples) == 0 {
		return Benchmark{}, ErrNoOffers
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Price.Minor < samples[j].Price.Minor })
	at := func(q float64) money.Money { return samples[int(q*float64(len(samples)-1))].Price }
	b := Benchmark{Category: category, Offers: len(samples), Min: samples[0].Price, P25: at(0.25),
		Median: at(0.5), Max: samples[len(samples)-1].Price, Samples: samples}
	if len(b.Samples) > 8 {
		b.Samples = b.Samples[:8]
	}
	return b, nil
}
