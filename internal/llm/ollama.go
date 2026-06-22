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

// OllamaProvider 调用本地 Ollama（OpenAI-compatible API 格式）。
type OllamaProvider struct {
	baseURL  string
	model    string
	client   *http.Client
}

func NewOllama(cfg *Config) (*OllamaProvider, error) {
	return &OllamaProvider{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		client:  &http.Client{Timeout: 300 * time.Second}, // 本地模型可能较慢
	}, nil
}

func (p *OllamaProvider) Name() string { return "ollama" }

// Generate 使用 Ollama 的 /v1/chat/completions 端点（与 OpenAI 兼容）。
func (p *OllamaProvider) Generate(ctx context.Context, system, user string) (string, error) {
	msgs := []openaiMsg{}
	if system != "" {
		msgs = append(msgs, openaiMsg{Role: "system", Content: system})
	}
	msgs = append(msgs, openaiMsg{Role: "user", Content: user})

	body, _ := json.Marshal(map[string]any{
		"model":    p.model,
		"messages": msgs,
	})

	u := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "poi-research/2.0 (+local)")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var r openaiResp
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("ollama parse error: %w | body: %s", err, string(b))
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("ollama returned no choices")
	}
	return r.Choices[0].Message.Content, nil
}
