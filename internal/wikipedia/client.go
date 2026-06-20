package wikipedia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"poi-research/internal/model"
)

type Client struct {
	httpClient *http.Client
	userAgent  string
}

func NewClient(userAgent string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		userAgent:  userAgent,
	}
}

type wikiSummaryResp struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Extract     string `json:"extract"`
	FullURL     string `json:"fullurl"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
	Status *string `json:"error"`
}

func (c *Client) SearchSummary(ctx context.Context, query, language string) (*model.WikipediaInfo, error) {
	if query == "" {
		return nil, nil
	}
	if language == "" {
		language = "en"
	}

	endpoint := fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s",
		language, url.PathEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("wikipedia status %d: %s", resp.StatusCode, string(b))
	}

	var body wikiSummaryResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Type == "https://mediawiki.org/wiki/HyperSwitch/errors/not_found" || body.Extract == "" {
		return nil, nil
	}

	url := body.FullURL
	if url == "" && body.ContentURLs.Desktop.Page != "" {
		url = body.ContentURLs.Desktop.Page
	}

	return &model.WikipediaInfo{
		Title:   body.Title,
		Summary: body.Extract,
		URL:     url,
	}, nil
}

func (c *Client) SearchByKeyword(ctx context.Context, keyword, language string) (*model.WikipediaInfo, error) {
	if keyword == "" {
		return nil, nil
	}
	if language == "" {
		language = "en"
	}
	time.Sleep(500 * time.Millisecond)

	searchURL := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=opensearch&search=%s&limit=3&format=json",
		language, url.QueryEscape(keyword))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var raw []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw) < 4 {
		return nil, nil
	}

	var titles []string
	if err := json.Unmarshal(raw[1], &titles); err != nil || len(titles) == 0 {
		return nil, nil
	}

	bestTitle := strings.TrimSpace(titles[0])
	if bestTitle == "" {
		return nil, nil
	}
	return c.SearchSummary(ctx, bestTitle, language)
}
