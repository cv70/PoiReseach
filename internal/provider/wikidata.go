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

// WikidataProvider 使用 Wikidata MediaWiki API 搜索实体并获取坐标。
// - wbsearchentities: 按关键词搜实体
// - wbgetentities: 查 P625 (坐标)、P31 (instance of)、P1376 (首都) 等属性
type WikidataProvider struct {
	httpClient *http.Client
	userAgent  string
	language   string
}

func NewWikidataProvider(userAgent, language string) *WikidataProvider {
	if language == "" {
		language = "zh"
	}
	return &WikidataProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  userAgent,
		language:   language,
	}
}

func (p *WikidataProvider) Name() string { return "wikidata" }

type searchItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Display     struct {
		Label struct {
			Value string `json:"value"`
		} `json:"label"`
		Description struct {
			Value string `json:"value"`
		} `json:"description"`
	} `json:"display"`
	ConceptURI string `json:"concepturi"`
}

type searchResp struct {
	Search []searchItem `json:"search"`
	SearchInfo struct {
		TotalHits int `json:"totalhits"`
	} `json:"searchinfo"`
}

type entity struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Labels map[string]struct {
		Value string `json:"value"`
	} `json:"labels"`
	Descriptions map[string]struct {
		Value string `json:"value"`
	} `json:"descriptions"`
	Claims map[string][]struct {
		Mainsnak struct {
			Datavalue struct {
				Value any `json:"value"`
			} `json:"datavalue"`
		} `json:"mainsnak"`
	} `json:"claims"`
}

type entitiesResp struct {
	Entities map[string]entity `json:"entities"`
}

func (p *WikidataProvider) Search(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if query == "" {
		return nil, fmt.Errorf("query empty")
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	time.Sleep(200 * time.Millisecond)

	// 1) 关键词搜实体
	q := url.Values{}
	q.Set("action", "wbsearchentities")
	q.Set("search", query)
	q.Set("language", p.language)
	q.Set("uselang", p.language)
	q.Set("type", "item")
	q.Set("limit", strconv.Itoa(limit*2)) // 多拿一些，过滤掉没坐标的
	q.Set("format", "json")
	q.Set("formatversion", "2")

	u := "https://www.wikidata.org/w/api.php?" + q.Encode()
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
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("wikidata search status %d: %s", resp.StatusCode, string(b))
	}

	var body searchResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	if len(body.Search) == 0 {
		// 回退：再用英文搜一次
		if p.language != "en" {
			return p.searchInLanguage(ctx, query, "en", limit)
		}
		return nil, nil
	}

	ids := make([]string, 0, len(body.Search))
	for _, s := range body.Search {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
		if len(ids) >= limit*2 {
			break
		}
	}

	// 2) 批量取实体属性
	entities, err := p.fetchEntities(ctx, ids)
	if err != nil {
		return nil, err
	}

	// 3) 组装结果：只保留有坐标的实体
	out := make([]model.Place, 0, len(entities))
	for idx, item := range body.Search {
		ent, ok := entities[item.ID]
		if !ok {
			continue
		}
		lat, lon, ok := extractCoord(ent)
		if !ok {
			continue
		}

		label := ""
		if l, ok := ent.Labels[p.language]; ok && l.Value != "" {
			label = l.Value
		} else if l, ok := ent.Labels["en"]; ok && l.Value != "" {
			label = l.Value
		}
		if label == "" {
			label = item.Display.Label.Value
		}
		if label == "" {
			label = item.Title
		}

		desc := ""
		if d, ok := ent.Descriptions[p.language]; ok && d.Value != "" {
			desc = d.Value
		} else if d, ok := ent.Descriptions["en"]; ok && d.Value != "" {
			desc = d.Value
		}

		// 通过 P31(instance of) 判断类别标签（简单映射：建筑/城市/地点/人物等 -> place）
		class, subType := classifyByP31(ent)

		importance := 0.45 - float64(idx)*0.03

		out = append(out, model.Place{
			RawID:       item.ID,
			Lat:         strconv.FormatFloat(lat, 'f', -1, 64),
			Lon:         strconv.FormatFloat(lon, 'f', -1, 64),
			DisplayName: label + " (" + desc + ")",
			Class:       class,
			Type:        subType,
			Importance:  importance,
			Address:     map[string]string{"wikidata_id": item.ID, "concept": item.ConceptURI},
			Tags: map[string]string{
				"wikidata":      item.ID,
				"description":   desc,
				"label_original": item.Display.Label.Value,
			},
			Source: p.Name(),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (p *WikidataProvider) searchInLanguage(ctx context.Context, query, lang string, limit int) ([]model.Place, error) {
	clone := NewWikidataProvider(p.userAgent, lang)
	clone.httpClient = p.httpClient
	return clone.Search(ctx, query, limit)
}

func (p *WikidataProvider) fetchEntities(ctx context.Context, ids []string) (map[string]entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("action", "wbgetentities")
	q.Set("ids", strings.Join(ids, "|"))
	q.Set("props", "claims|labels|descriptions")
	q.Set("format", "json")
	q.Set("formatversion", "2")
	q.Set("languages", p.language+"|en")

	u := "https://www.wikidata.org/w/api.php?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("wikidata entities status %d: %s", resp.StatusCode, string(b))
	}

	var body entitiesResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Entities, nil
}

// extractCoord 从 claims 中找出 P625 的经纬度。
func extractCoord(e entity) (float64, float64, bool) {
	claims, ok := e.Claims["P625"]
	if !ok || len(claims) == 0 {
		return 0, 0, false
	}
	raw := claims[0].Mainsnak.Datavalue.Value
	if raw == nil {
		return 0, 0, false
	}
	// raw 是 map[string]any: {"latitude": ..., "longitude": ...}
	m, ok := raw.(map[string]any)
	if !ok {
		return 0, 0, false
	}
	lat, latOK := parseFloat(m["latitude"])
	lon, lonOK := parseFloat(m["longitude"])
	if !latOK || !lonOK {
		return 0, 0, false
	}
	return lat, lon, true
}

// classifyByP31 根据 instance-of(P31) 映射出 class/type（启发式，不完美但够用）。
func classifyByP31(e entity) (string, string) {
	claims, ok := e.Claims["P31"]
	if !ok {
		return "tourism", "attraction"
	}
	// 取第一个实例的 ID，并映射
	for _, c := range claims {
		raw := c.Mainsnak.Datavalue.Value
		if raw == nil {
			continue
		}
		if m, ok := raw.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				if mapped, ok := wikidataIDToCategory[id]; ok {
					return mapped, id
				}
			}
		}
	}
	return "tourism", "attraction"
}

// wikidataIDToCategory 简化映射表（热门类型 -> 我们的 class 标签）
var wikidataIDToCategory = map[string]string{
	"Q515":        "place",   // city
	"Q5119":       "place",   // capital city
	"Q2039348":    "place",   // tourist attraction
	"Q570116":     "tourism", // tourist resort
	"Q839954":     "tourism", // archaeological site
	"Q13406463":   "tourism", // list of tourist attractions
	"Q33506":      "amenity", // museum
	"Q18660430":   "amenity", // cultural center
	"Q483453":     "amenity", // theatre
	"Q1248784":    "amenity", // place of worship
	"Q18601125":   "historic", // historic site
	"Q41176":      "building", // building
	"Q16917":      "building", // hospital
	"Q123705":     "shop",   // shopping mall
	"Q215380":     "leisure", // amusement park
	"Q22698":      "leisure", // park
	"Q152017":     "natural", // nature reserve
	"Q23413":      "natural", // mountain
	"Q4022":       "natural", // river
	"Q1752":       "natural", // lake
	"Q34225":      "natural", // island
	"Q532":        "natural", // village
	"Q3957":       "transport", // town
	"Q11707":      "transport", // airport
	"Q55488":      "transport", // railway station
}

func parseFloat(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}
