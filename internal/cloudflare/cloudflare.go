// Package cloudflare manages custom hostnames (Cloudflare for SaaS), so
// store owners can serve their app on their own domain.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the Cloudflare API for one zone (the apps domain).
type Client struct {
	Token   string // API token with SSL and Certificates Write on the zone
	ZoneID  string
	BaseURL string // default https://api.cloudflare.com/client/v4
	HTTP    *http.Client
}

// Hostname is a custom hostname and what it still needs.
type Hostname struct {
	ID                 string   `json:"id"`
	Hostname           string   `json:"hostname"`
	Status             string   `json:"status"`
	VerificationErrors []string `json:"verification_errors"`
	Ownership          struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"ownership_verification"`
	SSL struct {
		Status            string `json:"status"`
		ValidationRecords []struct {
			TxtName  string `json:"txt_name"`
			TxtValue string `json:"txt_value"`
		} `json:"validation_records"`
		ValidationErrors []struct {
			Message string `json:"message"`
		} `json:"validation_errors"`
	} `json:"ssl"`
}

// Active reports whether the hostname serves traffic with a certificate.
func (h Hostname) Active() bool { return h.Status == "active" && h.SSL.Status == "active" }

// Problems lists Cloudflare's reasons the hostname is not active yet.
func (h Hostname) Problems() []string {
	out := append([]string{}, h.VerificationErrors...)
	for _, e := range h.SSL.ValidationErrors {
		out = append(out, e.Message)
	}
	return out
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/zones/"+c.ZoneID+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: %w", err)
	}
	defer resp.Body.Close()
	var env struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("cloudflare answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if !env.Success {
		var msgs []string
		for _, e := range env.Errors {
			msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		return fmt.Errorf("cloudflare: %s", strings.Join(msgs, "; "))
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Create adds a custom hostname, validated over HTTP once its CNAME points
// at the zone's fallback origin.
func (c *Client) Create(ctx context.Context, host string) (Hostname, error) {
	var h Hostname
	err := c.do(ctx, http.MethodPost, "/custom_hostnames", map[string]any{
		"hostname": host, "ssl": map[string]any{"method": "http", "type": "dv", "settings": map[string]string{"min_tls_version": "1.2"}},
	}, &h)
	return h, err
}

// Get reads a custom hostname's current state.
func (c *Client) Get(ctx context.Context, id string) (Hostname, error) {
	var h Hostname
	return h, c.do(ctx, http.MethodGet, "/custom_hostnames/"+id, nil, &h)
}

// Delete removes a custom hostname.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/custom_hostnames/"+id, nil, nil)
}
