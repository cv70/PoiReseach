package service

import (
	"testing"

	"poi-research/internal/model"
)

func TestToPrimaryPlace(t *testing.T) {
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
			"phone": "+33 1 23 45 67 89",
		},
	}

	pp := toPrimaryPlace(p)
	if pp == nil {
		t.Fatal("expected non-nil primary place")
	}
	if pp.City != "Paris" {
		t.Errorf("city got %q", pp.City)
	}
	if pp.Category != "tourism" {
		t.Errorf("category got %q", pp.Category)
	}
	t.Logf("primary: %+v", pp)
}

func TestFilterOutSelf(t *testing.T) {
	pois := []model.POI{
		{Name: "Tour Eiffel"},
		{Name: "Musée d'Orsay"},
	}
	out := filterOutSelf(pois, "Tour Eiffel, Paris, France")
	if len(out) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(out))
	}
}

func TestWikiLang(t *testing.T) {
	p := model.Place{Address: map[string]string{"country_code": "cn"}}
	if wikiLang(p) != "zh" {
		t.Errorf("expected zh, got %s", wikiLang(p))
	}
	p2 := model.Place{Address: map[string]string{"country_code": "us"}}
	if wikiLang(p2) != "en" {
		t.Errorf("expected en, got %s", wikiLang(p2))
	}
}
