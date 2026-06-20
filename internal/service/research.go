package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"poi-research/internal/model"
	"poi-research/internal/nominatim"
	"poi-research/internal/overpass"
	"poi-research/internal/weather"
	"poi-research/internal/wikipedia"
)

type ResearchService struct {
	nominatim *nominatim.Client
	overpass  *overpass.Client
	wiki      *wikipedia.Client
	weather   *weather.Client
	userAgent string
}

func NewResearchService(userAgent string) *ResearchService {
	return &ResearchService{
		nominatim: nominatim.NewClient(userAgent),
		overpass:  overpass.NewClient(userAgent),
		wiki:      wikipedia.NewClient(userAgent),
		weather:   weather.NewClient(userAgent),
		userAgent: userAgent,
	}
}

// TravelResearch 旅游攻略专用的深度研究接口：返回主景点信息 + 按类别分组的周边 POI + 维基百科 + 天气 + 时区
func (s *ResearchService) TravelResearch(ctx context.Context, query string) (*model.TravelResult, error) {
	log.Printf("[travel] query=%q starting", query)

	places, err := s.nominatim.Search(ctx, query, 3)
	if err != nil {
		return nil, fmt.Errorf("nominatim search: %w", err)
	}
	if len(places) == 0 {
		return &model.TravelResult{Query: query}, nil
	}

	best := places[0]
	for _, p := range places {
		if p.Importance > best.Importance {
			best = p
		}
	}
	log.Printf("[travel] best match: %q (class=%s type=%s)", best.DisplayName, best.Class, best.Type)

	lat, _ := strconv.ParseFloat(best.Lat, 64)
	lon, _ := strconv.ParseFloat(best.Lon, 64)

	primary := buildPrimary(&best)
	result := &model.TravelResult{
		Query:   query,
		Primary: primary,
	}

	// 三路并发：周边 POI / 维基百科 / 天气&时区
	var (
		wg         sync.WaitGroup
		nearBundle *model.NearbyBundle
		wikiInfo   *model.WikipediaInfo
		wthInfo    *model.WeatherInfo
		tz         string
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		var flat []model.POI
		nearBundle, flat, _ = s.overpass.QueryCategorizedNearby(ctx, lat, lon)
		_ = flat // bundle 已经按类别分好
		// 过滤掉"自己"
		if nearBundle != nil {
			filterSelfFromBundle(nearBundle, best.DisplayName)
		}
	}()

	go func() {
		defer wg.Done()
		keyword := bestWikiKeyword(query, best)
		lang := wikiLang(best)
		if keyword != "" {
			wikiInfo, _ = s.wiki.SearchByKeyword(ctx, keyword, lang)
			// 回退：如果指定语言没结果，试试英文
			if wikiInfo == nil && lang != "en" {
				wikiInfo, _ = s.wiki.SearchByKeyword(ctx, keyword, "en")
			}
		}
	}()

	go func() {
		defer wg.Done()
		wthInfo, tz, _ = s.weather.Current(ctx, lat, lon)
	}()

	wg.Wait()

	result.Nearby = nearBundle
	result.Wikipedia = wikiInfo
	result.Weather = wthInfo
	result.Timezone = tz
	result.Tips = generateTravelTips(primary, nearBundle, wthInfo, wikiInfo)
	return result, nil
}

// DeepResearch 旧接口保留（返回扁平化结构，向后兼容）
func (s *ResearchService) DeepResearch(ctx context.Context, query string) (*model.DeepResearchResult, error) {
	tr, err := s.TravelResearch(ctx, query)
	if err != nil {
		return nil, err
	}
	var flat []model.POI
	if tr.Nearby != nil {
		flat = append(flat, tr.Nearby.Attractions...)
		flat = append(flat, tr.Nearby.Museums...)
		flat = append(flat, tr.Nearby.Restaurants...)
		flat = append(flat, tr.Nearby.Cafes...)
		flat = append(flat, tr.Nearby.BarsPubs...)
		flat = append(flat, tr.Nearby.Hotels...)
		flat = append(flat, tr.Nearby.Shops...)
		flat = append(flat, tr.Nearby.Transport...)
		flat = append(flat, tr.Nearby.Nature...)
		flat = append(flat, tr.Nearby.Others...)
	}
	return &model.DeepResearchResult{
		Query:      tr.Query,
		Primary:    tr.Primary,
		NearbyPOIs: flat,
		Wikipedia:  tr.Wikipedia,
		Weather:    tr.Weather,
		Timezone:   tr.Timezone,
	}, nil
}

// SearchOnly 只调用 Nominatim，返回候选地点列表
func (s *ResearchService) SearchOnly(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if limit <= 0 {
		limit = 5
	}
	return s.nominatim.Search(ctx, query, limit)
}

func buildPrimary(p *model.Place) *model.PrimaryPlace {
	if p == nil {
		return nil
	}
	tags := p.Tags
	name := extractNameFromDisplay(p.DisplayName)
	nameZh := ""
	if tags != nil {
		nameZh = firstNonEmptyInMap(tags, "name:zh", "name:zh-CN", "name:zh-Hans")
	}

	city := ""
	country := ""
	if p.Address != nil {
		city = firstNonEmptyInMap(p.Address, "city", "town", "village", "suburb")
		country = firstNonEmptyInMap(p.Address, "country", "country_code")
	}

	return &model.PrimaryPlace{
		Name:         name,
		NameZh:       nameZh,
		FullAddress:  p.DisplayName,
		Lat:          p.Lat,
		Lon:          p.Lon,
		Category:     p.Class,
		SubType:      p.Type,
		Phone:        firstNonEmptyInMap(tags, "phone", "contact:phone"),
		Website:      firstNonEmptyInMap(tags, "website", "contact:website", "url"),
		Email:        firstNonEmptyInMap(tags, "email", "contact:email"),
		OpeningHours: firstNonEmptyInMap(tags, "opening_hours"),
		Fee:          firstNonEmptyInMap(tags, "fee", "charge_fee", "tourism:fee"),
		Wheelchair:   firstNonEmptyInMap(tags, "wheelchair"),
		Cuisine:      firstNonEmptyInMap(tags, "cuisine"),
		City:         city,
		Country:      country,
		Postcode:     firstNonEmptyInMap(p.Address, "postcode", "postal_code"),
		Wikipedia:    firstNonEmptyInMap(tags, "wikipedia", "wikipedia:zh"),
		ImageURL:     firstNonEmptyInMap(tags, "image", "wikimedia_commons"),
	}
}

func bestWikiKeyword(query string, p model.Place) string {
	if p.Tags != nil && p.Tags["wikipedia"] != "" {
		raw := p.Tags["wikipedia"]
		if idx := strings.Index(raw, ":"); idx >= 0 {
			return strings.TrimSpace(raw[idx+1:])
		}
		return raw
	}
	// 用 OSM 显示名中的首段作为关键词
	name := extractNameFromDisplay(p.DisplayName)
	if name != "" {
		return name
	}
	return query
}

func wikiLang(p model.Place) string {
	lang := "en"
	if p.Address != nil {
		cc := strings.ToLower(p.Address["country_code"])
		switch cc {
		case "cn", "tw", "hk", "mo", "sg":
			lang = "zh"
		case "jp":
			lang = "ja"
		case "kr", "kp":
			lang = "ko"
		case "fr", "be", "ch":
			lang = "fr"
		case "de", "at":
			lang = "de"
		case "es", "ar", "cl", "mx", "co":
			lang = "es"
		case "ru", "by", "ua":
			lang = "ru"
		case "it":
			lang = "it"
		case "pt", "br":
			lang = "pt"
		case "nl":
			lang = "nl"
		}
	}
	return lang
}

func extractNameFromDisplay(d string) string {
	if d == "" {
		return ""
	}
	for _, sep := range []string{"，", ", "} {
		if idx := strings.Index(d, sep); idx >= 0 {
			return strings.TrimSpace(d[:idx])
		}
	}
	if idx := strings.Index(d, ","); idx >= 0 {
		return strings.TrimSpace(d[:idx])
	}
	return strings.TrimSpace(d)
}

func firstNonEmptyInMap(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

func filterSelfFromBundle(b *model.NearbyBundle, display string) {
	head := strings.ToLower(strings.TrimSpace(extractNameFromDisplay(display)))
	if head == "" {
		return
	}
	filter := func(pois []model.POI) []model.POI {
		out := pois[:0]
		for _, p := range pois {
			if strings.EqualFold(p.Name, head) {
				continue
			}
			out = append(out, p)
		}
		return out
	}
	b.Attractions = filter(b.Attractions)
	b.Museums = filter(b.Museums)
	b.Restaurants = filter(b.Restaurants)
	b.Cafes = filter(b.Cafes)
	b.BarsPubs = filter(b.BarsPubs)
	b.Hotels = filter(b.Hotels)
	b.Shops = filter(b.Shops)
	b.Transport = filter(b.Transport)
	b.Nature = filter(b.Nature)
	b.Others = filter(b.Others)
}

// generateTravelTips 基于聚合结果产出简单的中文旅游小贴士
func generateTravelTips(primary *model.PrimaryPlace, bundle *model.NearbyBundle, wth *model.WeatherInfo, wiki *model.WikipediaInfo) []string {
	var tips []string

	if primary != nil {
		if primary.OpeningHours != "" {
			tips = append(tips, fmt.Sprintf("开放时间：%s", primary.OpeningHours))
		}
		if primary.Fee != "" {
			tips = append(tips, fmt.Sprintf("费用信息：%s", primary.Fee))
		}
		if primary.Phone != "" {
			tips = append(tips, fmt.Sprintf("联系电话：%s", primary.Phone))
		}
		if primary.Website != "" {
			tips = append(tips, fmt.Sprintf("官方网站：%s", primary.Website))
		}
		if primary.Wheelchair != "" {
			tips = append(tips, fmt.Sprintf("无障碍：%s", primary.Wheelchair))
		}
	}

	if bundle != nil {
		if n := len(bundle.Attractions); n > 0 {
			tips = append(tips, fmt.Sprintf("周边 %d 处景点/地标值得一逛", n))
		}
		if n := len(bundle.Museums); n > 0 {
			tips = append(tips, fmt.Sprintf("附近有 %d 家博物馆/美术馆", n))
		}
		if n := len(bundle.Restaurants); n > 0 {
			tips = append(tips, fmt.Sprintf("附近有 %d 家餐厅，可结合美食安排", n))
		}
		if n := len(bundle.Cafes); n > 0 {
			tips = append(tips, fmt.Sprintf("周边有 %d 家咖啡馆", n))
		}
		if n := len(bundle.Hotels); n > 0 {
			tips = append(tips, fmt.Sprintf("周边有 %d 家酒店/旅馆可选", n))
		}
		if n := len(bundle.Transport); n > 0 {
			tips = append(tips, fmt.Sprintf("交通参考：周边有 %d 处交通站点（车站/地铁等）", n))
		}
		if n := len(bundle.Nature); n > 0 {
			tips = append(tips, fmt.Sprintf("自然休闲：附近有 %d 处公园/绿地/景区", n))
		}
	}

	if wth != nil {
		if wth.Temperature > 30 {
			tips = append(tips, fmt.Sprintf("当前气温 %.1f°C，天气炎热，注意防晒补水", wth.Temperature))
		} else if wth.Temperature < 5 {
			tips = append(tips, fmt.Sprintf("当前气温 %.1f°C，天气寒冷，注意保暖", wth.Temperature))
		} else {
			tips = append(tips, fmt.Sprintf("当前天气 %s，气温 %.1f°C，体感 %.1f°C", wth.Description, wth.Temperature, wth.FeelsLike))
		}
		if wth.WindSpeed > 30 {
			tips = append(tips, "风力较大，请注意防风")
		}
		if wth.Humidity > 80 {
			tips = append(tips, "湿度较高，体感偏闷热")
		}
	}

	if wiki != nil && wiki.Summary != "" {
		s := strings.TrimSpace(wiki.Summary)
		if len(s) > 220 {
			s = s[:220] + "..."
		}
		tips = append(tips, "维基百科简介："+s)
	}

	if len(tips) == 0 {
		tips = append(tips, "当前地点信息有限，建议进一步通过详细名称或经纬度查询")
	}
	return tips
}
