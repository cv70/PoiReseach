package nominatim

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
	baseURL    string
}

func NewClient(userAgent string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  userAgent,
		baseURL:    "https://nominatim.openstreetmap.org",
	}
}

type rawPlace struct {
	PlaceID     int64             `json:"place_id"`
	OSMType     string            `json:"osm_type"`
	OSMID       int64             `json:"osm_id"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	DisplayName string            `json:"display_name"`
	Class       string            `json:"class"`
	Type        string            `json:"type"`
	Importance  float64           `json:"importance"`
	Address     map[string]string `json:"address"`
	Extratags   map[string]string `json:"extratags"`
	BoundingBox []string          `json:"boundingbox"`
	Icon        string            `json:"icon"`
	Namedetails map[string]string `json:"namedetails"`
}

// Search 使用 Nominatim 搜索，返回多个候选地点
func (c *Client) Search(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	time.Sleep(1100 * time.Millisecond)

	u := fmt.Sprintf("%s/search?q=%s&format=jsonv2&limit=%d&addressdetails=1&extratags=1&namedetails=1&accept-language=zh-CN,en",
		c.baseURL, url.QueryEscape(query), limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nominatim search returned status %d: %s", resp.StatusCode, string(b))
	}

	var raw []rawPlace
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	result := make([]model.Place, 0, len(raw))
	for _, r := range raw {
		result = append(result, toPlace(r))
	}
	return result, nil
}

// LookupDetails 使用 osm_type + osm_id 查询更完整的地点详情
func (c *Client) LookupDetails(ctx context.Context, osmType string, osmID int64) (*model.PlaceDetail, error) {
	if osmType == "" || osmID == 0 {
		return nil, fmt.Errorf("osm type/id required")
	}
	time.Sleep(1100 * time.Millisecond)

	u := fmt.Sprintf("%s/lookup?osm_ids=%s%d&format=json&addressdetails=1&extratags=1&namedetails=1&accept-language=zh-CN,en",
		c.baseURL, mapType(osmType), osmID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nominatim lookup returned status %d: %s", resp.StatusCode, string(b))
	}

	var raw []rawPlace
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("no details found")
	}

	p := toPlace(raw[0])
	return &model.PlaceDetail{
		Place:     p,
		ExtraTags: raw[0].Extratags,
	}, nil
}

// ReverseByLatLon 经纬度反查
func (c *Client) ReverseByLatLon(ctx context.Context, lat, lon float64) (*model.Place, error) {
	time.Sleep(1100 * time.Millisecond)
	u := fmt.Sprintf("%s/reverse?format=jsonv2&lat=%.6f&lon=%.6f&addressdetails=1&extratags=1&namedetails=1&accept-language=zh-CN,en&zoom=18",
		c.baseURL, lat, lon)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nominatim reverse status %d: %s", resp.StatusCode, string(b))
	}
	var r rawPlace
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	p := toPlace(r)
	return &p, nil
}

func toPlace(r rawPlace) model.Place {
	nameZh := ""
	if r.Namedetails != nil {
		nameZh = firstNonEmpty(r.Namedetails, "name:zh", "name:zh-CN", "name:zh-Hans", "zh_name")
	}
	tags := make(map[string]string, len(r.Extratags)+1)
	for k, v := range r.Extratags {
		tags[k] = v
	}
	if nameZh != "" {
		tags["name:zh"] = nameZh
	}

	return model.Place{
		PlaceID:     fmt.Sprintf("%d", r.PlaceID),
		OSMType:     r.OSMType,
		OSMID:       r.OSMID,
		Lat:         r.Lat,
		Lon:         r.Lon,
		DisplayName: r.DisplayName,
		Class:       r.Class,
		Type:        r.Type,
		Importance:  r.Importance,
		Address:     r.Address,
		Tags:        tags,
		BoundingBox: r.BoundingBox,
		Icon:        r.Icon,
	}
}

func mapType(t string) string {
	switch strings.ToLower(t) {
	case "node":
		return "N"
	case "way":
		return "W"
	case "relation":
		return "R"
	default:
		return "N"
	}
}

func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
