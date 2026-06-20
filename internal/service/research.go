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

func (s *ResearchService) DeepResearch(ctx context.Context, query string) (*model.DeepResearchResult, error) {
	log.Printf("[research] query=%q starting", query)

	places, err := s.nominatim.Search(ctx, query, 3)
	if err != nil {
		return nil, fmt.Errorf("nominatim search: %w", err)
	}
	if len(places) == 0 {
		return &model.DeepResearchResult{Query: query}, nil
	}

	best := places[0]
	for _, p := range places {
		if p.Importance > best.Importance {
			best = p
		}
	}
	log.Printf("[research] best match: %q (class=%s type=%s)", best.DisplayName, best.Class, best.Type)

	lat, _ := strconv.ParseFloat(best.Lat, 64)
	lon, _ := strconv.ParseFloat(best.Lon, 64)

	result := &model.DeepResearchResult{
		Query:   query,
		Primary: toPrimaryPlace(&best),
	}

	var (
		wg            sync.WaitGroup
		pois          []model.POI
		wikiInfo      *model.WikipediaInfo
		weatherInfo   *model.WeatherInfo
		tz            string
		errPOI, errW, errWE error
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		pois, errPOI = s.overpass.QueryNearby(ctx, lat, lon, 1000, 50)
	}()

	go func() {
		defer wg.Done()
		keyword := bestKeyword(query, best)
		if keyword != "" {
			wikiInfo, errW = s.wiki.SearchByKeyword(ctx, keyword, wikiLang(best))
		}
	}()

	go func() {
		defer wg.Done()
		weatherInfo, tz, errWE = s.weather.Current(ctx, lat, lon)
	}()

	wg.Wait()

	if errPOI == nil {
		result.NearbyPOIs = filterOutSelf(pois, best.DisplayName)
	} else {
		log.Printf("[research] overpass error: %v", errPOI)
	}

	result.Wikipedia = wikiInfo
	if errW != nil {
		log.Printf("[research] wikipedia error: %v", errW)
	}

	result.Weather = weatherInfo
	result.Timezone = tz
	if errWE != nil {
		log.Printf("[research] weather error: %v", errWE)
	}

	return result, nil
}

func (s *ResearchService) SearchOnly(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if limit <= 0 {
		limit = 5
	}
	return s.nominatim.Search(ctx, query, limit)
}

func toPrimaryPlace(p *model.Place) *model.PrimaryPlace {
	if p == nil {
		return nil
	}
	tags := p.Tags
	name := tags["name:zh"]
	if name == "" {
		name = extractNameFromDisplay(p.DisplayName)
	}
	if name == "" {
		name = p.DisplayName
	}

	city := ""
	if p.Address != nil {
		city = firstNonEmpty(p.Address, "city", "town", "village", "suburb", "county")
	}

	return &model.PrimaryPlace{
		Name:         name,
		FullAddress:  p.DisplayName,
		Lat:          p.Lat,
		Lon:          p.Lon,
		Category:     p.Class,
		SubType:      p.Type,
		Phone:        tags["phone"],
		Website:      firstNonEmpty(tags, "website", "url"),
		OpeningHours: tags["opening_hours"],
		Email:        tags["email"],
		City:         city,
		Country:      firstNonEmpty(p.Address, "country", "country_code"),
		Postcode:     firstNonEmpty(p.Address, "postcode", "postal_code"),
		Wikipedia:    tags["wikipedia"],
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

func extractNameFromDisplay(d string) string {
	if d == "" {
		return ""
	}
	parts := strings.SplitN(d, ",", 2)
	return strings.TrimSpace(parts[0])
}

func bestKeyword(query string, p model.Place) string {
	if p.Tags != nil && p.Tags["wikipedia"] != "" {
		raw := p.Tags["wikipedia"]
		if idx := strings.Index(raw, ":"); idx >= 0 {
			return strings.TrimSpace(raw[idx+1:])
		}
		return raw
	}
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
		case "cn", "tw", "hk":
			lang = "zh"
		case "jp":
			lang = "ja"
		case "kr":
			lang = "ko"
		case "fr":
			lang = "fr"
		case "de":
			lang = "de"
		case "es":
			lang = "es"
		case "ru":
			lang = "ru"
		case "it":
			lang = "it"
		case "pt":
			lang = "pt"
		}
	}
	return lang
}

func filterOutSelf(pois []model.POI, display string) []model.POI {
	if display == "" || len(pois) == 0 {
		return pois
	}
	out := make([]model.POI, 0, len(pois))
	selfHead := strings.ToLower(strings.TrimSpace(extractNameFromDisplay(display)))
	for _, p := range pois {
		if strings.EqualFold(p.Name, selfHead) {
			continue
		}
		out = append(out, p)
	}
	return out
}
