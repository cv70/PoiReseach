package service

import (
	"context"
	"testing"

	"poi-research/internal/model"
	"poi-research/internal/provider"
)

// fakeProvider 是最小实现的测试桩。
type fakeProvider struct {
	name   string
	places []model.Place
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Search(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if len(f.places) == 0 {
		return nil, nil
	}
	out := make([]model.Place, len(f.places))
	copy(out, f.places)
	// 给每个元素填 source
	for i := range out {
		if out[i].Source == "" {
			out[i].Source = f.name
		}
	}
	return out, nil
}

func TestSearchOnlyFallsBackToFirstProvider(t *testing.T) {
	providers := []provider.PlaceProvider{
		&fakeProvider{
			name: "nominatim",
			places: []model.Place{{DisplayName: "Nominatim-Top", Importance: 0.8, Lat: "1.0", Lon: "2.0"}},
		},
	}
	svc := NewResearchServiceWithProviders("test", providers)
	out, err := svc.SearchOnly(context.Background(), "anything", 3)
	if err != nil {
		t.Fatalf("SearchOnly err=%v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected results")
	}
	if out[0].DisplayName != "Nominatim-Top" {
		t.Errorf("got %q", out[0].DisplayName)
	}
}

func TestMultiSearchMergesSources(t *testing.T) {
	providers := []provider.PlaceProvider{
		&fakeProvider{
			name: "nominatim",
			places: []model.Place{
				{DisplayName: "Tour Eiffel, Paris", Importance: 0.9, Lat: "48.8588", Lon: "2.2943", Class: "tourism", Type: "attraction"},
			},
		},
		&fakeProvider{
			name: "photon",
			places: []model.Place{
				// 同景点（近似坐标 + 相同首段名）应去重
				{DisplayName: "Tour Eiffel, 75007 Paris", Importance: 0.5, Lat: "48.8588", Lon: "2.2943", Class: "tourism", Type: "attraction"},
				// 另一个景点
				{DisplayName: "Louvre, Paris", Importance: 0.7, Lat: "48.8606", Lon: "2.3376", Class: "tourism", Type: "museum"},
			},
		},
		&fakeProvider{name: "wikidata", places: nil}, // 空结果应被安全忽略
	}
	svc := NewResearchServiceWithProviders("test", providers)
	out := svc.MultiSearch(context.Background(), "paris", 3)
	if len(out) != 2 {
		t.Errorf("expected 2 merged results, got %d: %+v", len(out), out)
	}
	// 第一条应该是 Importance 最高的 Tour Eiffel
	if out[0].Importance <= out[1].Importance {
		t.Errorf("expected sorted by importance, got %+v", out)
	}
	for _, p := range out {
		if p.Source == "" {
			t.Errorf("expected source to be set, got %+v", p)
		}
	}
}

func TestBuildPrimary(t *testing.T) {
	p := &model.Place{
		Lat:         "48.8588443",
		Lon:         "2.2943506",
		DisplayName: "Tour Eiffel, 5 Avenue Anatole France, 75007 Paris, France",
		Class:       "tourism",
		Type:        "attraction",
		Address: map[string]string{
			"city":    "Paris",
			"country": "France",
		},
		Tags: map[string]string{
			"phone":         "+33 1 23 45 67 89",
			"opening_hours": "Mo-Su 09:00-00:00",
			"wikipedia":     "fr:Tour Eiffel",
			"name:zh":       "埃菲尔铁塔",
		},
	}
	pp := buildPrimary(p)
	if pp == nil {
		t.Fatal("expected non-nil primary place")
	}
	if pp.City != "Paris" {
		t.Errorf("city got %q", pp.City)
	}
	if pp.Category != "tourism" {
		t.Errorf("category got %q", pp.Category)
	}
	if pp.NameZh != "埃菲尔铁塔" {
		t.Errorf("name_zh got %q", pp.NameZh)
	}
	if pp.Phone == "" {
		t.Errorf("phone should be extracted from tags")
	}
}

func TestGenerateTravelTips(t *testing.T) {
	primary := &model.PrimaryPlace{
		OpeningHours: "Mo-Su 09:00-18:00",
		Fee:          "yes",
		Phone:        "+33 1 23 45 67 89",
	}
	bundle := &model.NearbyBundle{
		Attractions: make([]model.POI, 5),
		Museums:     make([]model.POI, 3),
		Restaurants: make([]model.POI, 12),
		Transport:   make([]model.POI, 4),
	}
	wiki := &model.WikipediaInfo{Summary: "埃菲尔铁塔是位于法国巴黎第七区塞纳河南岸的一座镂空结构铁塔。"}
	tips := generateTravelTips(primary, bundle, nil, wiki)
	if len(tips) < 5 {
		t.Errorf("expect >=5 tips, got %d: %v", len(tips), tips)
	}
	for _, tip := range tips {
		t.Logf("tip: %s", tip)
	}
}

func TestBestWikiKeyword(t *testing.T) {
	p := model.Place{
		DisplayName: "Tour Eiffel, Paris, France",
		Tags:        map[string]string{"wikipedia": "fr:Tour Eiffel"},
	}
	k := bestWikiKeyword("Eiffel Tower", p)
	if k != "Tour Eiffel" {
		t.Errorf("got %q", k)
	}
}
