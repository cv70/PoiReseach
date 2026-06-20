package service

import (
	"strings"
	"testing"

	"poi-research/internal/model"
)

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
			"opening_hours":  "Mo-Su 09:00-00:00",
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
	if pp.Wikipedia != "fr:Tour Eiffel" {
		t.Errorf("wikipedia got %q", pp.Wikipedia)
	}
	if pp.NameZh != "埃菲尔铁塔" {
		t.Errorf("name_zh got %q", pp.NameZh)
	}
	if pp.Phone == "" {
		t.Errorf("phone should be extracted from tags")
	}
	t.Logf("primary: %+v", pp)
}

func TestFilterSelfFromBundle(t *testing.T) {
	b := &model.NearbyBundle{
		Attractions: []model.POI{{Name: "Tour Eiffel"}, {Name: "Arc de Triomphe"}},
		Restaurants: []model.POI{{Name: "Tour Eiffel"}},
		Museums:     []model.POI{{Name: "Louvre"}},
	}
	filterSelfFromBundle(b, "Tour Eiffel, Paris, France")
	if len(b.Attractions) != 1 || b.Attractions[0].Name != "Arc de Triomphe" {
		t.Errorf("attractions got %+v", b.Attractions)
	}
	if len(b.Restaurants) != 0 {
		t.Errorf("restaurants should be empty, got %+v", b.Restaurants)
	}
	if len(b.Museums) != 1 {
		t.Errorf("museums unchanged: %+v", b.Museums)
	}
}

func TestWikiLang(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"cn", "zh"}, {"jp", "ja"}, {"kr", "ko"},
		{"fr", "fr"}, {"de", "de"}, {"es", "es"},
		{"ru", "ru"}, {"it", "it"}, {"us", "en"},
	}
	for _, tc := range tests {
		p := model.Place{Address: map[string]string{"country_code": tc.in}}
		if got := wikiLang(p); got != tc.want {
			t.Errorf("wikiLang(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBestWikiKeyword(t *testing.T) {
	p := model.Place{
		DisplayName: "Tour Eiffel, 5 Avenue Anatole France, 75007 Paris, France",
		Tags:        map[string]string{"wikipedia": "fr:Tour Eiffel"},
	}
	k := bestWikiKeyword("Eiffel Tower", p)
	if k != "Tour Eiffel" {
		t.Errorf("got %q", k)
	}
	p2 := model.Place{DisplayName: "故宫，北京市"}
	k2 := bestWikiKeyword("故宫", p2)
	if k2 != "故宫" {
		t.Errorf("got %q", k2)
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
	wiki := &model.WikipediaInfo{Summary: "埃菲尔铁塔（La Tour Eiffel）是位于法国巴黎第七区塞纳河南岸的一座镂空结构铁塔，建于1887年至1889年间，高330米（含天线）。"}

	tips := generateTravelTips(primary, bundle, nil, wiki)
	if len(tips) < 5 {
		t.Errorf("expect >= 5 tips, got %d: %v", len(tips), tips)
	}
	for _, tip := range tips {
		t.Logf("tip: %s", tip)
	}
}

func TestExtractNameFromDisplay(t *testing.T) {
	cases := []string{"Tour Eiffel, Paris", "故宫，北京市", "Kyoto Station, Kyoto, Japan"}
	for _, c := range cases {
		n := extractNameFromDisplay(c)
		if n == "" || strings.Contains(n, ",") || strings.Contains(n, "，") {
			t.Errorf("extractNameFromDisplay(%q)=%q", c, n)
		}
	}
}
