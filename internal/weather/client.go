package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"poi-research/internal/model"
)

type Client struct {
	httpClient *http.Client
	userAgent  string
}

func NewClient(userAgent string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		userAgent:  userAgent,
	}
}

type openMeteoResp struct {
	Current struct {
		Temperature   float64 `json:"temperature_2m"`
		FeelsLike     float64 `json:"apparent_temperature"`
		Humidity      int     `json:"relative_humidity_2m"`
		WindSpeed     float64 `json:"wind_speed_10m"`
		Pressure      float64 `json:"pressure_msl"`
		WeatherCode   int     `json:"weather_code"`
	} `json:"current"`
	Timezone string `json:"timezone"`
}

func (c *Client) Current(ctx context.Context, lat, lon float64) (*model.WeatherInfo, string, error) {
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.6f&longitude=%.6f&current=temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,pressure_msl,weather_code&timezone=auto",
		lat, lon,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("open-meteo status %d: %s", resp.StatusCode, string(b))
	}

	var body openMeteoResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", err
	}

	return &model.WeatherInfo{
		Description: describeWeather(body.Current.WeatherCode),
		Temperature: body.Current.Temperature,
		FeelsLike:   body.Current.FeelsLike,
		Humidity:    body.Current.Humidity,
		WindSpeed:   body.Current.WindSpeed,
		Pressure:    body.Current.Pressure,
		Icon:        weatherIcon(body.Current.WeatherCode),
	}, body.Timezone, nil
}

func describeWeather(code int) string {
	switch {
	case code == 0:
		return "晴朗"
	case code <= 2:
		return "少云 / 部分多云"
	case code == 3:
		return "阴"
	case code <= 48:
		return "雾 / 冻雾"
	case code <= 57:
		return "毛毛雨"
	case code <= 67:
		return "雨"
	case code <= 77:
		return "雪"
	case code <= 82:
		return "阵雨"
	case code <= 86:
		return "阵雪"
	case code <= 99:
		return "雷暴"
	default:
		return "未知"
	}
}

func weatherIcon(code int) string {
	switch {
	case code == 0:
		return "☀️"
	case code <= 2:
		return "⛅"
	case code == 3:
		return "☁️"
	case code <= 48:
		return "🌫️"
	case code <= 67:
		return "🌧️"
	case code <= 77:
		return "❄️"
	case code <= 82:
		return "🌦️"
	case code <= 86:
		return "🌨️"
	case code <= 99:
		return "⛈️"
	default:
		return "🌡️"
	}
}
