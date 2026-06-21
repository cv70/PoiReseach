package provider

import (
	"context"

	"poi-research/internal/model"
	"poi-research/internal/nominatim"
)

// NominatimProvider 包装已有的 nominatim.Client。
type NominatimProvider struct {
	client *nominatim.Client
}

func NewNominatimProvider(userAgent string) *NominatimProvider {
	return &NominatimProvider{client: nominatim.NewClient(userAgent)}
}

func (p *NominatimProvider) Name() string { return "nominatim" }

func (p *NominatimProvider) Search(ctx context.Context, query string, limit int) ([]model.Place, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	places, err := p.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	for i := range places {
		places[i].Source = p.Name()
	}
	return places, nil
}
