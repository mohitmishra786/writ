// Package notify delivers step-up prompts to the principal's phone via ntfy
// (https://docs.ntfy.sh/publish/). A topic is effectively a password on the
// public server, so it must be long and random; the click URL is a Mandate
// short link, never a raw PayPal URL.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var topicRe = regexp.MustCompile(`^[A-Za-z0-9_-]{24,64}$`)

type Ntfy struct {
	server string
	topic  string
	http   *http.Client
}

// NewNtfy requires an unguessable topic (24-64 chars of [A-Za-z0-9_-]; the
// ntfy limit is 64).
func NewNtfy(server, topic string) (*Ntfy, error) {
	if !topicRe.MatchString(topic) {
		return nil, errors.New("notify: NTFY_TOPIC must be 24-64 chars of letters, digits, '_' or '-'")
	}
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &Ntfy{server: strings.TrimRight(server, "/"), topic: topic, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (n *Ntfy) Notify(ctx context.Context, title, message, clickURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.server+"/"+n.topic, strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("X-Title", title)
	req.Header.Set("X-Priority", "high")
	if clickURL != "" {
		req.Header.Set("X-Click", clickURL)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("notify: ntfy returned %d", resp.StatusCode)
	}
	return nil
}

// Discard is used when no notifier is configured.
type Discard struct{}

func (Discard) Notify(context.Context, string, string, string) error { return nil }
