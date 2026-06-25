package redfox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string

	maxRetries  int
	retryDelay  time.Duration
	rateLimiter *rateLimiter
}

type ClientOption func(*Client)

func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
		baseURL:    "https://redfox.hk/story/api",
		maxRetries: 3,
		retryDelay: 500 * time.Millisecond,
		rateLimiter: newRateLimiter(10, time.Second),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func NewClientFromEnv() (*Client, error) {
	apiKey := os.Getenv("REDFOX_API_KEY")
	if apiKey == "" {
		return nil, errors.New("REDFOX_API_KEY environment variable is not set")
	}
	baseURL := os.Getenv("REDFOX_BASE_URL")
	c := NewClient(apiKey)
	if baseURL != "" {
		c.baseURL = baseURL
	}
	return c, nil
}

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithMaxRetries(n int) ClientOption {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

func WithRetryDelay(d time.Duration) ClientOption {
	return func(c *Client) {
		if d > 0 {
			c.retryDelay = d
		}
	}
}

func WithRateLimit(requestsPerSec int) ClientOption {
	return func(c *Client) {
		if requestsPerSec > 0 {
			c.rateLimiter = newRateLimiter(requestsPerSec, time.Second)
		}
	}
}

func (c *Client) APIKey() string { return c.apiKey }

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) post(ctx context.Context, path string, body interface{}, result interface{}) error {
	if c.apiKey == "" {
		return fmt.Errorf("redfox API key is required")
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	url := c.baseURL + path

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		if err := c.rateLimiter.wait(ctx); err != nil {
			return err
		}

		apiResp, err := c.doRequest(ctx, url, bodyBytes)
		if err != nil {
			lastErr = err
			if isRetryableError(err) {
				continue
			}
			return err
		}

		if result != nil && apiResp.Data != nil {
			dataBytes, err := json.Marshal(apiResp.Data)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(dataBytes, result); err != nil {
				return fmt.Errorf("decode data: %w", err)
			}
		}
		return nil
	}

	return fmt.Errorf("redfox %s failed after %d retries: %w", path, c.maxRetries, lastErr)
}

func (c *Client) doRequest(ctx context.Context, url string, bodyBytes []byte) (*apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			HTTPStatus: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if apiResp.Code != 0 && apiResp.Code != 200 {
		return nil, &APIError{
			HTTPStatus: resp.StatusCode,
			Code:       apiResp.Code,
			Message:    apiResp.Message,
		}
	}

	return &apiResp, nil
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type APIError struct {
	HTTPStatus int    `json:"http_status"`
	Code       int    `json:"code,omitempty"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("redfox API error: http=%d, code=%d, message=%s", e.HTTPStatus, e.Code, e.Message)
	}
	return fmt.Sprintf("redfox HTTP %d: %s", e.HTTPStatus, e.Message)
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatus >= 500 || apiErr.HTTPStatus == http.StatusTooManyRequests
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return false
}

type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	permits  int
	tokens   int
	lastFill time.Time
}

func newRateLimiter(rate int, interval time.Duration) *rateLimiter {
	return &rateLimiter{
		interval: interval,
		permits:  rate,
		tokens:   rate,
		lastFill: time.Now(),
	}
}

func (r *rateLimiter) wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(r.lastFill)
		if elapsed >= r.interval {
			r.tokens = r.permits
			r.lastFill = now
		}
		if r.tokens > 0 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}
		waitTime := r.interval - elapsed
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
}
