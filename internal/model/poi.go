package model

type Place struct {
	PlaceID       string            `json:"place_id,omitempty"`
	OSMType     string            `json:"osm_type,omitempty"`
	OSMID       int64             `json:"osm_id,omitempty"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	DisplayName string            `json:"display_name,omitempty"`
	Class       string            `json:"class,omitempty"`
	Type        string            `json:"type,omitempty"`
	TypeEn      string            `json:"type_en,omitempty"`
	Importance  float64           `json:"importance,omitempty"`
	Address     map[string]string `json:"address,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	BoundingBox []string         `json:"boundingbox,omitempty"`
	LICNREF    string            `json:"licence_ref,omitempty"`
	Icon        string            `json:"icon,omitempty"`
}

type PlaceDetail struct {
	Place
	ExtraTags     map[string]string `json:"extra_tags,omitempty"`
	Extratags   map[string]string `json:"extratags,omitempty"`
}

type POI struct {
	Name     string            `json:"name"`
	Category string            `json:"category"`
	Type     string            `json:"type"`
	Lat      string            `json:"lat"`
	Lon      string            `json:"lon"`
	Address  string            `json:"address"`
	Tags     map[string]string `json:"tags"`
}

type DeepResearchResult struct {
	Query      string             `json:"query"`
	Primary    *PrimaryPlace       `json:"primary_place"`
	NearbyPOIs []POI             `json:"nearby_pois"`
	Wikipedia  *WikipediaInfo     `json:"wikipedia,omitempty"`
	Weather    *WeatherInfo      `json:"weather,omitempty"`
	Timezone   string            `json:"timezone,omitempty"`
}

type PrimaryPlace struct {
	Name        string            `json:"name"`
	FullAddress string            `json:"full_address"`
	Lat         string          `json:"lat"`
	Lon         string          `json:"lon"`
	Category    string            `json:"category"`
	SubType     string            `json:"sub_type"`
	Phone       string            `json:"phone,omitempty"`
	Website     string            `json:"website,omitempty"`
	OpeningHours string           `json:"opening_hours,omitempty"`
	Email       string            `json:"email,omitempty"`
	City        string            `json:"city,omitempty"`
	Country     string            `json:"country,omitempty"`
	Postcode    string            `json:"postcode,omitempty"`
	Wikipedia   string            `json:"wikipedia,omitempty"`
}

type WikipediaInfo struct {
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	URL         string `json:"url"`
}

type WeatherInfo struct {
	Description string  `json:"description"`
	Temperature float64 `json:"temperature_c"`
	FeelsLike   float64 `json:"feels_like_c"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed_ms"`
	Pressure    float64 `json:"pressure_hpa"`
	Icon        string  `json:"icon"`
}
