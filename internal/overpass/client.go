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
		httpClient: &http.Client{Timeout: 90 * time.Second},
		userAgent:  userAgent,
		baseURL:    "https://overpass-api.de/api/interpreter",
	}
}

type overpassElement struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Lat  float64           `json:"lat,omitempty"`
	Lon  float64           `json:"lon,omitempty"`
	Center *struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"center,omitempty"`
	Tags map[string]string `json:"tags,omitempty"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

// POICategory 定义一个旅游相关的分类查询
type POICategory struct {
	Name   string
	Radius int // 米
	Limit  int
	Filter string // Overpass tag filter，例如 "tourism=attraction"
	// 可组合多个 filter，任意一个命中即算
	Filters []string
}

// DefaultCategories 旅游攻略常用的分类
var DefaultCategories = []POICategory{
	{Name: "attractions", Radius: 2000, Limit: 25, Filters: []string{
		"tourism=attraction", "tourism=viewpoint", "tourism=information",
		"tourism=artwork", "tourism=theme_park", "historic=*",
		"man_made=tower", "man_made=obelisk", "landmark=*",
	}},
	{Name: "museums", Radius: 2000, Limit: 20, Filters: []string{
		"tourism=museum", "tourism=gallery", "amenity=arts_centre",
		"shop=books", // 书店也勉强相关，但单独又不太好，这里用专用字段
	}},
	{Name: "restaurants", Radius: 1500, Limit: 30, Filters: []string{
		"amenity=restaurant", "amenity=fast_food",
	}},
	{Name: "cafes", Radius: 1000, Limit: 20, Filters: []string{
		"amenity=cafe", "shop=bakery", "shop=pastry",
	}},
	{Name: "bars_pubs", Radius: 1500, Limit: 20, Filters: []string{
		"amenity=bar", "amenity=pub", "amenity=biergarten", "amenity=nightclub",
	}},
	{Name: "hotels", Radius: 2000, Limit: 20, Filters: []string{
		"tourism=hotel", "tourism=guest_house", "tourism=hostel",
		"tourism=motel", "tourism=apartment", "tourism=chalet",
	}},
	{Name: "shops", Radius: 1500, Limit: 25, Filters: []string{
		"shop=department_store", "shop=mall", "shop=supermarket",
		"shop=convenience", "shop=gift", "shop=clothes",
	}},
	{Name: "transport", Radius: 2500, Limit: 20, Filters: []string{
		"public_transport=station", "railway=station",
		"highway=bus_stop", "aeroway=aerodrome", "aeroway=terminal",
		"amenity=parking", "amenity=bicycle_rental", "amenity=car_rental",
	}},
	{Name: "nature", Radius: 3000, Limit: 20, Filters: []string{
		"leisure=park", "leisure=garden", "leisure=picnic_site",
		"leisure=nature_reserve", "natural=park", "natural=wood",
		"natural=beach", "waterway=riverbank", "place=island",
	}},
}

// QueryCategorizedNearby 按分类执行多个 Overpass 查询，并返回分组结果
// 注意：为避免上游压力过大，查询按顺序串行执行，已内置节流
func (c *Client) QueryCategorizedNearby(ctx context.Context, lat, lon float64) (*model.NearbyBundle, []model.POI, error) {
	bundle := &model.NearbyBundle{}
	var flat []model.POI

	for _, cat := range DefaultCategories {
		pois, err := c.queryCategory(ctx, cat, lat, lon)
		if err != nil {
			// 某一类失败不影响其它类
			continue
		}
		switch cat.Name {
		case "attractions":
			bundle.Attractions = pois
		case "museums":
			bundle.Museums = pois
		case "restaurants":
			bundle.Restaurants = pois
		case "cafes":
			bundle.Cafes = pois
		case "bars_pubs":
			bundle.BarsPubs = pois
		case "hotels":
			bundle.Hotels = pois
		case "shops":
			bundle.Shops = pois
		case "transport":
			bundle.Transport = pois
		case "nature":
			bundle.Nature = pois
		default:
			bundle.Others = append(bundle.Others, pois...)
		}
		flat = append(flat, pois...)
	}
	return bundle, flat, nil
}

func (c *Client) queryCategory(ctx context.Context, cat POICategory, lat, lon float64) ([]model.POI, error) {
	time.Sleep(600 * time.Millisecond)

	query := buildCategoryOverpassQL(cat, lat, lon)
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
		return nil, fmt.Errorf("overpass [%s] status %d: %s", cat.Name, resp.StatusCode, string(b))
	}

	var body overpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return toPOIs(body.Elements, cat.Name), nil
}

func buildCategoryOverpassQL(cat POICategory, lat, lon float64) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[out:json][timeout:25];\n"))
	sb.WriteString("(\n")
	for _, f := range cat.Filters {
		key, val, hasOp := splitKV(f)
		if hasOp {
			// 支持 "key=value", "key=*"
			sb.WriteString(fmt.Sprintf("  node[%s](around:%d,%.6f,%.6f);\n", renderFilter(key, val), cat.Radius, lat, lon))
			sb.WriteString(fmt.Sprintf("  way[%s](around:%d,%.6f,%.6f);\n", renderFilter(key, val), cat.Radius, lat, lon))
			sb.WriteString(fmt.Sprintf("  relation[%s](around:%d,%.6f,%.6f);\n", renderFilter(key, val), cat.Radius, lat, lon))
		}
	}
	sb.WriteString(");\n")
	sb.WriteString(fmt.Sprintf("out center tags %d;\n", cat.Limit))
	return sb.String()
}

func splitKV(s string) (key, val string, ok bool) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

func renderFilter(key, val string) string {
	if val == "*" {
		return fmt.Sprintf(`"%s"`, key)
	}
	return fmt.Sprintf(`"%s"="%s"`, key, val)
}

func toPOIs(elements []overpassElement, category string) []model.POI {
	pois := make([]model.POI, 0, len(elements))
	seen := make(map[string]struct{})
	for _, e := range elements {
		name := pickName(e.Tags)
		if name == "" {
			continue
		}
		key := fmt.Sprintf("%s|%d", category, e.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		lat, lon := e.Lat, e.Lon
		if (lat == 0 && lon == 0) && e.Center != nil {
			lat, lon = e.Center.Lat, e.Center.Lon
		}

		subType := inferSubType(e.Tags)
		city := firstNonEmpty(e.Tags, "addr:city", "addr:suburb", "addr:town")
		address := buildAddress(e.Tags)

		// 收集旅游相关的关键标签，保留原始 tags 便于下游使用
		relevant := pickRelevantTags(e.Tags)

		pois = append(pois, model.POI{
			Name:         name,
			NameZh:       firstNonEmpty(e.Tags, "name:zh", "name:zh-CN", "name:zh-Hans"),
			Category:     category,
			SubType:      subType,
			Lat:          strconv.FormatFloat(lat, 'f', -1, 64),
			Lon:          strconv.FormatFloat(lon, 'f', -1, 64),
			Address:      address,
			City:         city,
			Phone:        firstNonEmpty(e.Tags, "phone", "contact:phone"),
			Website:      firstNonEmpty(e.Tags, "website", "contact:website", "url"),
			Email:        firstNonEmpty(e.Tags, "email", "contact:email"),
			OpeningHours: e.Tags["opening_hours"],
			Cuisine:      firstNonEmpty(e.Tags, "cuisine", "diet:vegan"),
			Fee:          firstNonEmpty(e.Tags, "fee", "tourism:fee", "charge_fee"),
			Wheelchair:   e.Tags["wheelchair"],
			InternetAcc:  firstNonEmpty(e.Tags, "internet_access", "wifi"),
			Smoking:      e.Tags["smoking"],
			Dogs:         e.Tags["dog"],
			Stars:        e.Tags["stars"],
			Rating:       firstNonEmpty(e.Tags, "rating", "fhrs:rating"),
			Operator:     e.Tags["operator"],
			Brand:        firstNonEmpty(e.Tags, "brand", "brand:name"),
			ImageURL:     firstNonEmpty(e.Tags, "image", "wikimedia_commons", "photo"),
			Wikipedia:    firstNonEmpty(e.Tags, "wikipedia", "wikipedia:zh"),
			Description:  firstNonEmpty(e.Tags, "description", "description:zh", "note"),
			Tags:         relevant,
		})
	}
	return pois
}

func pickName(tags map[string]string) string {
	if v, ok := tags["name"]; ok && v != "" {
		return v
	}
	for _, k := range []string{"name:zh", "name:zh-CN", "name:en", "alt_name", "official_name"} {
		if v, ok := tags[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func inferSubType(tags map[string]string) string {
	keys := []string{
		"tourism", "amenity", "shop", "leisure", "historic",
		"office", "natural", "man_made", "craft", "sport", "highway",
		"railway", "public_transport", "building", "waterway",
	}
	for _, k := range keys {
		if v, ok := tags[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// relevantTagKeys 在 POI.Tags 中保留给用户看的一组"精选标签"
var relevantTagKeys = []string{
	"phone", "website", "email", "opening_hours", "cuisine",
	"fee", "wheelchair", "internet_access", "wifi", "smoking",
	"dog", "stars", "rating", "operator", "brand",
	"wikipedia", "image", "description", "capacity",
	"outdoor_seating", "takeaway", "delivery", "drive_through",
	"toilets", "toilets:wheelchair", "changing_table",
	"addr:housenumber", "addr:street", "addr:city", "addr:postcode",
	"payment:credit_cards", "payment:cash", "payment:contactless",
	"diet:vegetarian", "diet:vegan", "diet:gluten_free",
	"min_age", "max_age", "surveillance",
	"name:zh", "name:en", "short_name",
}

func pickRelevantTags(tags map[string]string) map[string]string {
	out := make(map[string]string, len(relevantTagKeys))
	for _, k := range relevantTagKeys {
		if v, ok := tags[k]; ok && v != "" {
			out[k] = v
		}
	}
	return out
}

func buildAddress(tags map[string]string) string {
	parts := []string{}
	for _, k := range []string{"addr:housenumber", "addr:street", "addr:suburb", "addr:city", "addr:state", "addr:postcode"} {
		if v, ok := tags[k]; ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// QueryNearby 为旧接口保留：一次混合查询，返回扁平 POI 列表
func (c *Client) QueryNearby(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]model.POI, error) {
	time.Sleep(600 * time.Millisecond)
	query := fmt.Sprintf(`[out:json][timeout:25];
(
  node["name"](around:%d,%.6f,%.6f);
  way["name"](around:%d,%.6f,%.6f);
  relation["name"](around:%d,%.6f,%.6f);
);
out center tags %d;
`, radiusMeters, lat, lon, radiusMeters, lat, lon, radiusMeters, lat, lon, limit)

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
		return nil, fmt.Errorf("overpass status %d: %s", resp.StatusCode, string(b))
	}
	var body overpassResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return toPOIs(body.Elements, "mixed"), nil
}
