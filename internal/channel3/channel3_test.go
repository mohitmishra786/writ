package channel3

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func product(id, cat string, prices ...float64) Product {
	p := Product{ID: id, Title: id, Category: &Category{Slug: cat}}
	for _, pr := range prices {
		p.Offers = append(p.Offers, Offer{Domain: "shop.example", Price: Price{Price: pr, Currency: "USD"}, Availability: "InStock"})
	}
	return p
}

func TestBenchmark(t *testing.T) {
	ps := []Product{
		product("a", "frothing-pitchers", 19.99, 66.99),
		product("b", "frothing-pitchers", 26.25),
		product("c", "frothing-pitchers", 79.99),
		product("d", "espresso-machines", 559.99), // other category: excluded
	}
	ps[1].Offers = append(ps[1].Offers, Offer{Price: Price{Price: 5, Currency: "USD"}, Availability: "OutOfStock"})
	b, err := BenchmarkOf("frothing-pitchers", ps)
	if err != nil {
		t.Fatal(err)
	}
	if b.Offers != 4 || b.Min.Minor != 1999 || b.Median.Minor != 2625 || b.Max.Minor != 7999 {
		t.Fatalf("benchmark %+v", b)
	}
	if _, err := BenchmarkOf("frothing-pitchers", nil); !errors.Is(err, ErrNoOffers) {
		t.Fatal("expected ErrNoOffers")
	}
}

func TestSearchRequestShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.URL.Path != "/v1/search" {
			w.WriteHeader(401)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f := body["filters"].(map[string]any)
		if f["category_ids"].([]any)[0] != "frothing-pitchers" || body["limit"].(float64) != 30 {
			t.Errorf("unexpected body %s", raw)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"products": []Product{product("a", "frothing-pitchers", 20)}})
	}))
	defer srv.Close()
	c := New("k")
	c.base = srv.URL
	ps, err := c.Search(context.Background(), SearchRequest{Query: "milk frothing pitcher", CategorySlug: "frothing-pitchers", Limit: 50})
	if err != nil || len(ps) != 1 {
		t.Fatalf("%v %v", ps, err)
	}
}

// Live check (1 search credit). Run with MANDATE_CHANNEL3_IT=1.
func TestLiveBenchmark(t *testing.T) {
	if os.Getenv("MANDATE_CHANNEL3_IT") != "1" {
		t.Skip("set MANDATE_CHANNEL3_IT=1 to call Channel3")
	}
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cats, err := c.SearchCategories(ctx, "frothing pitcher")
	if err != nil || len(cats) == 0 || cats[0].Slug != "frothing-pitchers" {
		t.Fatalf("categories %v %v", cats, err)
	}
	ps, err := c.Search(ctx, SearchRequest{Query: "stainless steel milk frothing pitcher", CategorySlug: cats[0].Slug, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	b, err := BenchmarkOf(cats[0].Slug, ps)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("offers=%d min=%s p25=%s median=%s max=%s", b.Offers, b.Min, b.P25, b.Median, b.Max)
}
