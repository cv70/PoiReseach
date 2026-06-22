package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ClaudeProvider struct {
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
}

func NewClaude(cfg *Config) (*ClaudeProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
	}
	return &ClaudeProvider{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *ClaudeProvider) Name() string { return "claude" }

type claudeMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeReq struct {
	Model         string        `json:"model"`
	MaxTokens     int           `json:"max_tokens"`
	System        string        `json:"system,omitempty"`
	Messages      []claudeMsg   `json:"messages"`
}

type claudeResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *ClaudeProvider) Generate(ctx context.Context, system, user string) (string, error) {
	msgs := []claudeMsg{}
	if system != "" {
		msgs = append(msgs, claudeMsg{Role: "user", Content: "[系统提示]\n" + system})
	}
	msgs = append(msgs, claudeMsg{Role: "user", Content: user})

	body, _ := json.Marshal(claudeReq{
		Model:     p.model,
		MaxTokens: 4096,
		System:    system,
		Messages:  msgs,
	})

	u := p.baseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("User-Agent", "poi-research/2.0 (+local)")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var r claudeResp
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("claude parse error: %w | body: %s", err, string(b))
	}
	if r.Error != nil {
		return "", fmt.Errorf("claude error [%s]: %s", r.Error.Type, r.Error.Message)
	}
	for _, c := range r.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("claude returned no text content")
}
