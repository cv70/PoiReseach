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

// OpenAICompatProvider 兼容所有 OpenAI-format 的国产/第三方 API。
// 例如：硅基流动 (siliconflow.cn)、Groq、Together AI、DeepSeek 等。
type OpenAICompatProvider struct {
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
}

func NewOpenAICompat(cfg *Config) (*OpenAICompatProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required for OpenAI-compatible provider")
	}
	return &OpenAICompatProvider{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *OpenAICompatProvider) Name() string { return "openai-compat" }

func (p *OpenAICompatProvider) Generate(ctx context.Context, system, user string) (string, error) {
	msgs := []openaiMsg{}
	if system != "" {
		msgs = append(msgs, openaiMsg{Role: "system", Content: system})
	}
	msgs = append(msgs, openaiMsg{Role: "user", Content: user})

	body, _ := json.Marshal(map[string]any{
		"model":     p.model,
		"messages":  msgs,
		"max_tokens": 4096,
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
		return "", fmt.Errorf("openai-compat request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var r openaiResp
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("openai-compat parse error: %w | body: %s", err, string(b))
	}
	if r.Error != nil {
		return "", fmt.Errorf("openai-compat error: %s", r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("openai-compat returned no choices")
	}
	return r.Choices[0].Message.Content, nil
}
