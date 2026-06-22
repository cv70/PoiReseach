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

type OpenAIProvider struct {
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
}

func NewOpenAI(cfg *Config) (*OpenAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	return &OpenAIProvider{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *OpenAIProvider) Name() string { return "openai" }

type openaiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiReq struct {
	Model    string        `json:"model"`
	Messages []openaiMsg   `json:"messages"`
	MaxTokens int         `json:"max_tokens,omitempty"`
}

type openaiResp struct {
	Choices []struct {
		Message openaiMsg `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) Generate(ctx context.Context, system, user string) (string, error) {
	msgs := []openaiMsg{}
	if system != "" {
		msgs = append(msgs, openaiMsg{Role: "system", Content: system})
	}
	msgs = append(msgs, openaiMsg{Role: "user", Content: user})

	body, _ := json.Marshal(openaiReq{
		Model:     p.model,
		Messages:  msgs,
		MaxTokens: 4096,
	})

	u := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("User-Agent", "poi-research/2.0 (+local)")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var r openaiResp
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("openai parse error: %w | body: %s", err, string(b))
	}
	if r.Error != nil {
		return "", fmt.Errorf("openai error: %s", r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return r.Choices[0].Message.Content, nil
}
