package provider

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

// PhotonProvider 调用 komoot Photon 开源地理搜索 API。
// 文档：https://photon.komoot.io/api/
type PhotonProvider struct {
	httpClient *http.Client
	userAgent  string
	baseURL    string
	language   string // 默认 zh，可通过 Search 参数覆盖
}

func NewPhotonProvider(userAgent, language string) *PhotonProvider {
	if language == "" {
		language = "zh"
	}
	return &PhotonProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  userAgent,
		baseURL:    "https://photon.komoot.io/api/",
		language:   language,
	}
}

func (p *PhotonProvider) Name() string { return "photon" }

type photonFeature struct {
	Type       string `json:"type"`
	Geometry   struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"` // [lon, lat]
	} `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type photonResp struct {
	Type     string           `json:"type"`
	Features []photonFeature  `json:"features"`
}

func (p *PhotonProvider) Search(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if query == "" {
		return nil, fmt.Errorf("query empty")
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	// Photon 节流：避免把服务打挂（非强制但礼貌）
	time.Sleep(400 * time.Millisecond)

	q := url.Values{}
	q.Set("q", query)
	q.Set("lang", p.language)
	q.Set("limit", strconv.Itoa(limit))

	u := p.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("photon status %d: %s", resp.StatusCode, string(body))
	}

	var body photonResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	places := make([]model.Place, 0, len(body.Features))
	for _, f := range body.Features {
		// GeoJSON coordinates: [lon, lat]
		var lat, lon float64
		if len(f.Geometry.Coordinates) >= 2 {
			lon = f.Geometry.Coordinates[0]
			lat = f.Geometry.Coordinates[1]
		}

		props := f.Properties
		addr := make(map[string]string, 8)
		for k, v := range map[string]string{
			"city":      toString(props["city"]),
			"state":     toString(props["state"]),
			"district":  toString(props["district"]),
			"country":   toString(props["country"]),
			"postcode":  toString(props["postcode"]),
			"street":    toString(props["street"]),
			"housenumber": toString(props["housenumber"]),
			"country_code": strings.ToLower(toString(props["countrycode"])),
		} {
			if v != "" {
				addr[k] = v
			}
		}

		name := toString(props["name"])
		if name == "" {
			if v, ok := props["street"]; ok && v != nil {
				name = fmt.Sprintf("%v", v)
			} else {
				continue
			}
		}

		display := buildPhotonDisplay(name, addr)
		class := toString(props["type"]) // 如 tourism, highway, place, building ...
		subType := toString(props["osm_value"])

		// importance 估算：Photon 自带顺序就是 relevance，越靠前 importance 越高
		importance := 0.5 - float64(len(places))*0.02

		places = append(places, model.Place{
			RawID:       toString(props["osm_id"]),
			OSMType:     osmTypeLetter(toString(props["osm_type"])),
			Lat:         strconv.FormatFloat(lat, 'f', -1, 64),
			Lon:         strconv.FormatFloat(lon, 'f', -1, 64),
			DisplayName: display,
			Class:       class,
			Type:        subType,
			Importance:  importance,
			Address:     addr,
			Source:      p.Name(),
		})
	}
	return places, nil
}

func buildPhotonDisplay(name string, addr map[string]string) string {
	parts := []string{name}
	for _, k := range []string{"city", "state", "country"} {
		if addr[k] != "" {
			parts = append(parts, addr[k])
		}
	}
	return strings.Join(parts, ", ")
}

func osmTypeLetter(s string) string {
	switch strings.ToUpper(s) {
	case "N", "NODE":
		return "node"
	case "W", "WAY":
		return "way"
	case "R", "RELATION":
		return "relation"
	}
	return ""
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
