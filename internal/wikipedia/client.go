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

type summaryResp struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	DisplayTitle string `json:"displaytitle"`
	Extract   string `json:"extract"`
	ExtractHTML string `json:"extract_html"`
	FullURL   string `json:"fullurl"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
	Thumbnail *struct {
		Source string `json:"source"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnail"`
	OriginalImage *struct {
		Source string `json:"source"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"originalimage"`
}

// SearchSummary 给定标题（精确的 Wikipedia 页面名），返回摘要 + 图片
func (c *Client) SearchSummary(ctx context.Context, title, language string) (*model.WikipediaInfo, error) {
	if title == "" {
		return nil, nil
	}
	if language == "" {
		language = "en"
	}

	endpoint := fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s",
		language, url.PathEscape(title))

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
		return nil, fmt.Errorf("wikipedia summary status %d: %s", resp.StatusCode, string(b))
	}

	var body summaryResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Type == "https://mediawiki.org/wiki/HyperSwitch/errors/not_found" || body.Extract == "" {
		return nil, nil
	}

	info := &model.WikipediaInfo{
		Title:       firstNonEmpty(body.DisplayTitle, body.Title),
		Summary:     body.Extract,
		ExtractHTML: body.ExtractHTML,
		URL:         firstNonEmpty(body.FullURL, body.ContentURLs.Desktop.Page),
	}
	if body.OriginalImage != nil && body.OriginalImage.Source != "" {
		info.ImageURL = body.OriginalImage.Source
	} else if body.Thumbnail != nil && body.Thumbnail.Source != "" {
		info.ImageURL = body.Thumbnail.Source
	}
	return info, nil
}

// SearchByKeyword 先用搜索接口找到最佳匹配的 Wikipedia 页面标题，再查摘要
func (c *Client) SearchByKeyword(ctx context.Context, keyword, language string) (*model.WikipediaInfo, error) {
	if keyword == "" {
		return nil, nil
	}
	if language == "" {
		language = "en"
	}
	time.Sleep(500 * time.Millisecond)

	// 使用 opensearch 找到候选标题
	searchURL := fmt.Sprintf(
		"https://%s.wikipedia.org/w/api.php?action=opensearch&search=%s&limit=5&format=json&redirects=resolve",
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

	info, err := c.SearchSummary(ctx, bestTitle, language)
	if err != nil || info == nil {
		return info, err
	}

	// 再拿几张附加图片（可选，不阻塞主流程）
	if images, imgErr := c.fetchPageImages(ctx, bestTitle, language); imgErr == nil && len(images) > 0 {
		if info.ImageURL == "" {
			info.ImageURL = images[0]
		}
		if len(images) > 1 {
			info.ImageList = images[1:min(len(images), 5)]
		}
	}
	return info, nil
}

type imagesQueryResp struct {
	Query struct {
		Pages map[string]struct {
			Title string `json:"title"`
			Images []struct {
				Title string `json:"title"`
			} `json:"images"`
		} `json:"pages"`
	} `json:"query"`
}

func (c *Client) fetchPageImages(ctx context.Context, title, language string) ([]string, error) {
	queryURL := fmt.Sprintf(
		"https://%s.wikipedia.org/w/api.php?action=query&titles=%s&prop=images&imlimit=10&format=json",
		language, url.QueryEscape(title))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var body imagesQueryResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var fileTitles []string
	for _, p := range body.Query.Pages {
		for _, img := range p.Images {
			name := img.Title
			// 过滤明显非图片的文件（如图标）
			if strings.HasPrefix(name, "File:") || strings.HasPrefix(name, "ファイル:") {
				fileTitles = append(fileTitles, name)
			}
		}
	}
	if len(fileTitles) == 0 {
		return nil, nil
	}

	// 把 File:XXX 转换成直接可用的缩略图 URL
	var out []string
	for _, name := range fileTitles[:min(len(fileTitles), 6)] {
		thumb, err := c.resolveCommonsThumbnail(ctx, name, 640)
		if err == nil && thumb != "" {
			out = append(out, thumb)
		}
	}
	return out, nil
}

type commonsResp struct {
	Query struct {
		Pages map[string]struct {
			Title     string `json:"title"`
			Thumbnail *struct {
				Source string `json:"source"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnail"`
		} `json:"pages"`
	} `json:"query"`
}

func (c *Client) resolveCommonsThumbnail(ctx context.Context, fileTitle string, maxPx int) (string, error) {
	u := fmt.Sprintf(
		"https://commons.wikimedia.org/w/api.php?action=query&titles=%s&prop=imageinfo&iiprop=url&iiurlwidth=%d&format=json",
		url.QueryEscape(fileTitle), maxPx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	// 直接用简单的字符串搜索拿到 imageinfo.thumburl
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var raw commonsResp
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", err
	}
	for _, p := range raw.Query.Pages {
		if p.Thumbnail != nil && p.Thumbnail.Source != "" {
			return p.Thumbnail.Source, nil
		}
	}
	return "", nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
