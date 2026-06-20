package overpass

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
		httpClient: &http.Client{Timeout: 60 * time.Second},
		userAgent:  userAgent,
		baseURL:    "https://overpass-api.de/api/interpreter",
	}
}

type overpassElement struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Lat  float64           `json:"lat,omitempty"`
	Lon  float64           `json:"lon,omitempty"`
	Tags map[string]string `json:"tags,omitempty"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

func (c *Client) QueryNearby(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]model.POI, error) {
	time.Sleep(600 * time.Millisecond)

	query := buildOverpassQL(lat, lon, radiusMeters, limit)

	form := url.Values{}
	form.Set("data", query)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("overpass returned status %d: %s", resp.StatusCode, string(b))
	}

	var body overpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return toPOIs(body.Elements), nil
}

func buildOverpassQL(lat, lon float64, radius int, limit int) string {
	r := fmt.Sprintf("%d", radius)
	coord := fmt.Sprintf("%.6f,%.6f", lat, lon)

	return fmt.Sprintf(`[out:json][timeout:25];
(
  node["name"](around:%s,%s);
  way["name"](around:%s,%s);
  relation["name"](around:%s,%s);
);
out center tags %d;
`, r, coord, r, coord, r, coord, limit)
}

func toPOIs(elements []overpassElement) []model.POI {
	pois := make([]model.POI, 0, len(elements))
	seen := make(map[string]struct{})
	for _, e := range elements {
		name := e.Tags["name"]
		if name == "" {
			name = e.Tags["name:zh"]
		}
		if name == "" {
			continue
		}
		key := fmt.Sprintf("%s|%d", name, e.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		latStr := strconv.FormatFloat(e.Lat, 'f', -1, 64)
		lonStr := strconv.FormatFloat(e.Lon, 'f', -1, 64)
		category := inferCategory(e.Tags)
		subType := inferSubType(e.Tags)

		address := buildAddress(e.Tags)

		pois = append(pois, model.POI{
			Name:     name,
			Category: category,
			Type:     subType,
			Lat:      latStr,
			Lon:      lonStr,
			Address:  address,
			Tags:     e.Tags,
		})
	}
	return pois
}

func inferCategory(tags map[string]string) string {
	if v, ok := tags["tourism"]; ok && v != "" {
		return "tourism"
	}
	if v, ok := tags["amenity"]; ok && v != "" {
		return "amenity"
	}
	if v, ok := tags["shop"]; ok && v != "" {
		return "shop"
	}
	if v, ok := tags["leisure"]; ok && v != "" {
		return "leisure"
	}
	if v, ok := tags["historic"]; ok && v != "" {
		return "historic"
	}
	if v, ok := tags["building"]; ok && v != "" {
		return "building"
	}
	if v, ok := tags["office"]; ok && v != "" {
		return "office"
	}
	if v, ok := tags["natural"]; ok && v != "" {
		return "natural"
	}
	return "other"
}

func inferSubType(tags map[string]string) string {
	keys := []string{"tourism", "amenity", "shop", "leisure", "historic", "building", "office", "natural", "man_made", "craft", "sport"}
	for _, k := range keys {
		if v, ok := tags[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func buildAddress(tags map[string]string) string {
	parts := []string{}
	for _, k := range []string{"addr:housenumber", "addr:street", "addr:suburb", "addr:city", "addr:state", "addr:country", "addr:postcode"} {
		if v, ok := tags[k]; ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}
