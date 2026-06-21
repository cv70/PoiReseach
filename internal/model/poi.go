package model

type Place struct {
	PlaceID     string            `json:"place_id,omitempty"`
	OSMType     string            `json:"osm_type,omitempty"`
	OSMID       int64             `json:"osm_id,omitempty"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	DisplayName string            `json:"display_name,omitempty"`
	Class       string            `json:"class,omitempty"`
	Type        string            `json:"type,omitempty"`
	Importance  float64           `json:"importance,omitempty"`
	Address     map[string]string `json:"address,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	BoundingBox []string          `json:"boundingbox,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Source      string            `json:"source,omitempty"`   // 数据来源: nominatim / photon / wikidata ...
	RawID       string            `json:"raw_id,omitempty"`  // 该源的原生 ID
}

type PlaceDetail struct {
	Place
	ExtraTags map[string]string `json:"extra_tags,omitempty"`
}

// POI 是通用的兴趣点（Overpass 返回）
type POI struct {
	Name        string            `json:"name"`
	NameZh      string            `json:"name_zh,omitempty"`
	Category    string            `json:"category"`
	SubType     string            `json:"type"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	Address     string            `json:"address,omitempty"`
	City        string            `json:"city,omitempty"`
	Phone       string            `json:"phone,omitempty"`
	Website     string            `json:"website,omitempty"`
	Email       string            `json:"email,omitempty"`
	OpeningHours string           `json:"opening_hours,omitempty"`
	Cuisine     string            `json:"cuisine,omitempty"`
	Fee         string            `json:"fee,omitempty"`
	Wheelchair  string            `json:"wheelchair,omitempty"`
	InternetAcc string            `json:"internet_access,omitempty"`
	Smoking     string            `json:"smoking,omitempty"`
	Dogs        string            `json:"dogs,omitempty"`
	Stars       string            `json:"stars,omitempty"`
	Rating      string            `json:"rating,omitempty"`
	Operator    string            `json:"operator,omitempty"`
	Brand       string            `json:"brand,omitempty"`
	ImageURL    string            `json:"image_url,omitempty"`
	Wikipedia   string            `json:"wikipedia,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// PrimaryPlace 聚合后的"主景点"信息（面向旅游攻略）
type PrimaryPlace struct {
	Name        string `json:"name"`
	NameZh      string `json:"name_zh,omitempty"`
	FullAddress string `json:"full_address"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Category    string `json:"category"`
	SubType     string `json:"sub_type"`

	Phone       string `json:"phone,omitempty"`
	Website     string `json:"website,omitempty"`
	Email       string `json:"email,omitempty"`
	OpeningHours string `json:"opening_hours,omitempty"`
	Fee         string `json:"fee,omitempty"`
	Wheelchair  string `json:"wheelchair,omitempty"`
	Cuisine     string `json:"cuisine,omitempty"`

	City        string `json:"city,omitempty"`
	Country     string `json:"country,omitempty"`
	Postcode    string `json:"postcode,omitempty"`

	Wikipedia   string `json:"wikipedia,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

// WikipediaInfo 维基百科摘要 + 图片
type WikipediaInfo struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	URL         string   `json:"url"`
	ImageURL    string   `json:"image_url,omitempty"`
	ExtractHTML string   `json:"extract_html,omitempty"`
	ImageList   []string `json:"images,omitempty"`
}

// WeatherInfo 实时天气
type WeatherInfo struct {
	Description string  `json:"description"`
	Temperature float64 `json:"temperature_c"`
	FeelsLike   float64 `json:"feels_like_c"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed_ms"`
	Pressure    float64 `json:"pressure_hpa"`
	Icon        string  `json:"icon"`
}

// TravelResult 旅游攻略专用响应
type TravelResult struct {
	Query     string         `json:"query"`
	Primary   *PrimaryPlace  `json:"primary_place"`
	Nearby    *NearbyBundle  `json:"nearby,omitempty"`
	Wikipedia *WikipediaInfo `json:"wikipedia,omitempty"`
	Weather   *WeatherInfo   `json:"weather,omitempty"`
	Timezone  string         `json:"timezone,omitempty"`
	Tips      []string       `json:"travel_tips,omitempty"`
}

// DeepResearchResult 之前的通用响应（保持向后兼容）
type DeepResearchResult struct {
	Query      string         `json:"query"`
	Primary    *PrimaryPlace  `json:"primary_place"`
	NearbyPOIs []POI          `json:"nearby_pois"`
	Wikipedia  *WikipediaInfo `json:"wikipedia,omitempty"`
	Weather    *WeatherInfo   `json:"weather,omitempty"`
	Timezone   string         `json:"timezone,omitempty"`
}

// NearbyBundle 按类别分组的周边 POI
type NearbyBundle struct {
	Attractions []POI `json:"attractions,omitempty"` // 景点 / 地标
	Museums     []POI `json:"museums,omitempty"`     // 博物馆/美术馆
	Restaurants []POI `json:"restaurants,omitempty"` // 餐厅
	Cafes       []POI `json:"cafes,omitempty"`       // 咖啡馆
	BarsPubs    []POI `json:"bars_pubs,omitempty"`   // 酒吧
	Hotels      []POI `json:"hotels,omitempty"`      // 酒店/旅馆
	Shops       []POI `json:"shops,omitempty"`       // 商店/购物中心
	Transport   []POI `json:"transport,omitempty"`   // 车站/地铁/机场
	Nature      []POI `json:"nature,omitempty"`      // 公园/自然景观
	Others      []POI `json:"others,omitempty"`      // 其他
}
