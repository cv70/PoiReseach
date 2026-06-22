package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// LLM 是统一的大模型接口。
type LLM interface {
	Name() string
	Generate(ctx context.Context, system, user string) (string, error)
}

// Config 描述 LLM 连接配置。
type Config struct {
	Provider string // openai | claude | ollama | openai-compat
	APIKey   string
	BaseURL  string // 可选，覆盖默认地址
	Model    string // 如 gpt-4o / claude-3-5-sonnet / qwen2.5 / deepseek-chat
}

// FromEnv 从环境变量构建 Config。
// 优先级：显式字段 > 环境变量。
func (c *Config) FromEnv() *Config {
	if c.Provider == "" {
		c.Provider = getEnv("LLM_PROVIDER", "openai")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("OPENAI_API_KEY")
		if c.APIKey == "" {
			c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
			if c.APIKey != "" && c.Provider == "openai" {
				c.Provider = "claude"
			}
		}
	}
	if c.BaseURL == "" {
		switch c.Provider {
		case "openai":
			c.BaseURL = getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1")
		case "claude":
			c.BaseURL = getEnv("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1")
		case "ollama":
			c.BaseURL = getEnv("OLLAMA_BASE_URL", "http://localhost:11434/v1")
		case "openai-compat":
			c.BaseURL = getEnv("OPENAI_COMPAT_BASE_URL", "https://api.siliconflow.cn/v1")
			if c.APIKey == "" {
				c.APIKey = os.Getenv("SILICONFLOW_API_KEY")
			}
		}
	}
	if c.Model == "" {
		switch c.Provider {
		case "openai":
			c.Model = getEnv("OPENAI_MODEL", "gpt-4o-mini")
		case "claude":
			c.Model = getEnv("ANTHROPIC_MODEL", "claude-3-5-sonnet-20241022")
		case "ollama":
			c.Model = getEnv("OLLAMA_MODEL", "qwen2.5")
		case "openai-compat":
			c.Model = getEnv("OPENAI_COMPAT_MODEL", "Qwen/Qwen2.5-7B-Instruct")
		}
	}
	return c
}

// Build 根据 Config 构造对应的 LLM 实例。
func Build(cfg *Config) (LLM, error) {
	switch cfg.Provider {
	case "openai":
		return NewOpenAI(cfg)
	case "claude":
		return NewClaude(cfg)
	case "ollama":
		return NewOllama(cfg)
	case "openai-compat":
		return NewOpenAICompat(cfg)
	default:
		return nil, fmt.Errorf("unknown LLM provider %q", cfg.Provider)
	}
}

// BestEffort 按优先级尝试初始化一个 LLM，直到成功。
// 遍历顺序：openai → claude → openai-compat → ollama。
func BestEffort() (LLM, string) {
	providers := []struct {
		provider string
		apiKey   string
		baseURL  string
		model    string
	}{
		{"openai", os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_BASE_URL"), os.Getenv("OPENAI_MODEL")},
		{"claude", os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("ANTHROPIC_BASE_URL"), os.Getenv("ANTHROPIC_MODEL")},
		{"openai-compat", os.Getenv("SILICONFLOW_API_KEY"), os.Getenv("OPENAI_COMPAT_BASE_URL"), os.Getenv("OPENAI_COMPAT_MODEL")},
		{"ollama", "", os.Getenv("OLLAMA_BASE_URL"), os.Getenv("OLLAMA_MODEL")},
	}
	for _, p := range providers {
		if p.provider == "openai" || p.provider == "claude" || p.provider == "openai-compat" {
			if p.apiKey == "" {
				continue
			}
		}
		cfg := (&Config{Provider: p.provider, APIKey: p.apiKey, BaseURL: p.baseURL, Model: p.model}).FromEnv()
		llm, err := Build(cfg)
		if err == nil && llm != nil {
			return llm, cfg.Provider
		}
	}
	return nil, ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DetectLanguage 简单语言检测，用于选择 prompt 语言。
func DetectLanguage(text string) string {
	hasCn := strings.ContainsAny(text, "\u4e00-\u9fff")
	if hasCn {
		return "zh"
	}
	return "en"
}
