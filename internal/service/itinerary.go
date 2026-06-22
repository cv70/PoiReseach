package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"poi-research/internal/llm"
	"poi-research/internal/model"
)

// ItineraryService 基于 LLM 生成 AI 旅游行程推荐。
type ItineraryService struct {
	llm    llm.LLM
	llmName string
}

// NewItineraryService 用全局 LLM 实例构造行程服务。
// llm 为 nil 时自动尝试 BestEffort 初始化。
func NewItineraryService(llmInst llm.LLM) (*ItineraryService, error) {
	if llmInst != nil {
		return &ItineraryService{
			llm:    llmInst,
			llmName: llmInst.Name(),
		}, nil
	}
	inst, name := llm.BestEffort()
	if inst == nil {
		return nil, fmt.Errorf("no LLM available: set OPENAI_API_KEY / ANTHROPIC_API_KEY / SILICONFLOW_API_KEY / OLLAMA_BASE_URL")
	}
	return &ItineraryService{
		llm:    inst,
		llmName: name,
	}, nil
}

// GenerateItinerary 根据旅游攻略数据生成一日/多日行程推荐。
// 可通过 options 传入旅行偏好。
func (s *ItineraryService) GenerateItinerary(ctx context.Context, travelResult *model.TravelResult, options *ItineraryOptions) (string, error) {
	if s.llm == nil {
		return "", fmt.Errorf("LLM not configured")
	}
	if options == nil {
		options = &ItineraryOptions{}
	}

	prompt := s.buildPrompt(travelResult, options)
	systemPrompt := s.buildSystem(travelResult.Query, options)

	result, err := s.llm.Generate(ctx, systemPrompt, prompt)
	if err != nil {
		log.Printf("[itinerary] LLM (%s) error: %v", s.llmName, err)
		return "", fmt.Errorf("LLM generation failed: %w", err)
	}
	return result, nil
}

// ItineraryOptions 描述行程生成偏好。
type ItineraryOptions struct {
	Days         int    // 计划天数，默认 1
	TripType     string // 自由行 / 亲子 / 情侣 / 商务 / 户外，默认自由行
	Budget       string // 经济 / 适中 / 奢侈，默认适中
	Language     string // zh / en
	IncludePhoto bool   // 是否在输出中包含图片 URL 引用
}

// ItineraryResult 是 /api/itinerary 的响应结构。
type ItineraryResult struct {
	Query   string `json:"query"`
	LLM     string `json:"llm_provider"`
	Days    int    `json:"days"`
	TripType string `json:"trip_type"`
	Context  struct {
		Primary     *model.PrimaryPlace   `json:"primary_place,omitempty"`
		Nearby     *model.NearbyBundle   `json:"nearby,omitempty"`
		Weather    *model.WeatherInfo     `json:"weather,omitempty"`
		Timezone   string                `json:"timezone,omitempty"`
		Wikipedia  *model.WikipediaInfo  `json:"wikipedia,omitempty"`
	} `json:"context,omitempty"`
	Itinerary string `json:"itinerary"`
}

// ---------------------- Prompt 构造 ----------------------

func (s *ItineraryService) buildSystem(query string, opts *ItineraryOptions) string {
	lang := llm.DetectLanguage(query)
	if opts.Language != "" {
		lang = opts.Language
	}
	if lang == "zh" {
		return `你是一位专业的中文旅游攻略规划师。请根据提供的景点和周边信息，为用户规划一条精心设计的旅行路线。

要求：
1. 输出结构化的行程安排（建议按时间线/路线节点组织）
2. 每天的行程包含：时间段、景点/活动、推荐理由、预计停留时长
3. 结合天气情况给出穿着建议（如果提供了天气信息）
4. 适当推荐餐厅和咖啡馆，但不要喧宾夺主
5. 标注哪些景点适合拍照/打卡
6. 如有交通不便之处，提前提醒
7. 最后给出 2-3 条实用 tips
8. 输出使用中文，语言自然流畅，不要太生硬
9. 行程控制在推荐天数内，不要过度安排
10. 如有门票/费用信息，标注清楚`
	}
	return `You are a professional English travel planner. Generate a well-structured travel itinerary based on the provided attractions and nearby information.

Requirements:
1. Output a structured day-by-day plan with time slots, attractions, reasons, and estimated duration
2. Give clothing advice based on weather if available
3. Recommend restaurants and cafes without overshadowing main attractions
4. Mark must-see photo spots
5. Warn about any transport inconveniences
6. End with 2-3 practical tips
7. Keep itinerary within recommended days, do not over-schedule
8. If admission fees are available, note them clearly`
}

func (s *ItineraryService) buildPrompt(r *model.TravelResult, opts *ItineraryOptions) string {
	lang := llm.DetectLanguage(r.Query)
	if opts.Language != "" {
		lang = opts.Language
	}

	var sb strings.Builder

	// 旅行偏好
	days := opts.Days
	if days <= 0 {
		days = 1
	}
	tripType := opts.TripType
	if tripType == "" {
		tripType = "自由行"
	}
	budget := opts.Budget
	if budget == "" {
		budget = "适中"
	}
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("用户计划前往【%s】旅行，旅行类型：%s，预算：%s，计划天数：%d天。\n\n", r.Query, tripType, budget, days))
	} else {
		sb.WriteString(fmt.Sprintf("User plans to visit 【%s】, trip type: %s, budget: %s, duration: %d day(s).\n\n", r.Query, tripType, budget, days))
	}

	// 主景点信息
	if r.Primary != nil {
		if lang == "zh" {
			sb.WriteString("=== 目标景点 ===\n")
			sb.WriteString(fmt.Sprintf("名称: %s", r.Primary.Name))
			if r.Primary.NameZh != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", r.Primary.NameZh))
			}
			sb.WriteString("\n")
			if r.Primary.FullAddress != "" {
				sb.WriteString(fmt.Sprintf("地址: %s\n", r.Primary.FullAddress))
			}
			if r.Primary.OpeningHours != "" {
				sb.WriteString(fmt.Sprintf("开放时间: %s\n", r.Primary.OpeningHours))
			}
			if r.Primary.Fee != "" {
				sb.WriteString(fmt.Sprintf("门票: %s\n", r.Primary.Fee))
			}
			if r.Primary.Website != "" {
				sb.WriteString(fmt.Sprintf("官网: %s\n", r.Primary.Website))
			}
			if r.Primary.Wheelchair != "" {
				sb.WriteString(fmt.Sprintf("无障碍设施: %s\n", r.Primary.Wheelchair))
			}
		} else {
			sb.WriteString("=== Primary Destination ===\n")
			sb.WriteString(fmt.Sprintf("Name: %s\n", r.Primary.Name))
			if r.Primary.FullAddress != "" {
				sb.WriteString(fmt.Sprintf("Address: %s\n", r.Primary.FullAddress))
			}
			if r.Primary.OpeningHours != "" {
				sb.WriteString(fmt.Sprintf("Hours: %s\n", r.Primary.OpeningHours))
			}
			if r.Primary.Fee != "" {
				sb.WriteString(fmt.Sprintf("Admission: %s\n", r.Primary.Fee))
			}
		}
	}

	// 天气
	if r.Weather != nil && lang == "zh" {
		sb.WriteString(fmt.Sprintf("\n=== 当前天气 ===\n%s，气温 %.1f°C（体感 %.1f°C），湿度 %d%%，风速 %.1fm/s，气压 %.0fhPa\n",
			r.Weather.Description, r.Weather.Temperature, r.Weather.FeelsLike,
			r.Weather.Humidity, r.Weather.WindSpeed, r.Weather.Pressure))
	} else if r.Weather != nil {
		sb.WriteString(fmt.Sprintf("\n=== Current Weather ===\n%s, %.1f°C (feels like %.1f°C), humidity %d%%\n",
			r.Weather.Description, r.Weather.Temperature, r.Weather.FeelsLike, r.Weather.Humidity))
	}

	// 时区
	if r.Timezone != "" {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("当地时区: %s\n", r.Timezone))
		} else {
			sb.WriteString(fmt.Sprintf("Local timezone: %s\n", r.Timezone))
		}
	}

	// 维基百科简介
	if r.Wikipedia != nil && r.Wikipedia.Summary != "" {
		if lang == "zh" {
			sb.WriteString("\n=== 景点简介 ===\n")
		} else {
			sb.WriteString("\n=== About this place ===\n")
		}
		summary := r.Wikipedia.Summary
		if len(summary) > 600 {
			summary = summary[:600] + "..."
		}
		sb.WriteString(summary + "\n")
	}

	// 周边景点
	if r.Nearby != nil {
		if lang == "zh" {
			sb.WriteString("\n=== 周边景点/地标 ===\n")
		} else {
			sb.WriteString("\n=== Nearby Attractions ===\n")
		}
		for _, p := range r.Nearby.Attractions {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	// 博物馆
	if r.Nearby != nil && len(r.Nearby.Museums) > 0 {
		if lang == "zh" {
			sb.WriteString("\n=== 博物馆/美术馆 ===\n")
		} else {
			sb.WriteString("\n=== Museums & Galleries ===\n")
		}
		for _, p := range r.Nearby.Museums {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	// 餐厅
	if r.Nearby != nil && len(r.Nearby.Restaurants) > 0 {
		if lang == "zh" {
			sb.WriteString("\n=== 推荐餐厅 ===\n")
		} else {
			sb.WriteString("\n=== Recommended Restaurants ===\n")
		}
		for _, p := range r.Nearby.Restaurants {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	// 咖啡馆
	if r.Nearby != nil && len(r.Nearby.Cafes) > 0 {
		if lang == "zh" {
			sb.WriteString("\n=== 咖啡馆/甜品店 ===\n")
		} else {
			sb.WriteString("\n=== Cafes & Desserts ===\n")
		}
		for _, p := range r.Nearby.Cafes {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	// 酒店
	if r.Nearby != nil && len(r.Nearby.Hotels) > 0 {
		if lang == "zh" {
			sb.WriteString("\n=== 周边住宿 ===\n")
		} else {
			sb.WriteString("\n=== Nearby Hotels ===\n")
		}
		for _, p := range r.Nearby.Hotels {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	// 自然景观
	if r.Nearby != nil && len(r.Nearby.Nature) > 0 {
		if lang == "zh" {
			sb.WriteString("\n=== 自然景观/公园 ===\n")
		} else {
			sb.WriteString("\n=== Nature & Parks ===\n")
		}
		for _, p := range r.Nearby.Nature {
			summarizePOI(&sb, &p, lang)
			if sb.Len() > 8000 {
				break
			}
		}
	}

	return sb.String()
}

func summarizePOI(sb *strings.Builder, p *model.POI, lang string) {
	if p == nil {
		return
	}
	name := p.Name
	if p.NameZh != "" {
		name = p.Name + " (" + p.NameZh + ")"
	}
	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("- %s [%s]\n", name, p.SubType))
	} else {
		sb.WriteString(fmt.Sprintf("- %s [%s]\n", name, p.SubType))
	}

	// 地址
	if p.Address != "" {
		sb.WriteString(fmt.Sprintf("  地址: %s\n", p.Address))
	}
	// 营业时间
	if p.OpeningHours != "" {
		sb.WriteString(fmt.Sprintf("  营业: %s\n", p.OpeningHours))
	}
	// 菜系 / 类型
	if p.Cuisine != "" {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("  类型: %s\n", p.Cuisine))
		} else {
			sb.WriteString(fmt.Sprintf("  Cuisine: %s\n", p.Cuisine))
		}
	}
	// 费用
	if p.Fee != "" {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("  费用: %s\n", p.Fee))
		} else {
			sb.WriteString(fmt.Sprintf("  Fee: %s\n", p.Fee))
		}
	}
	// 无障碍
	if p.Wheelchair != "" {
		sb.WriteString(fmt.Sprintf("  无障碍: %s\n", p.Wheelchair))
	}
	// 官网
	if p.Website != "" {
		sb.WriteString(fmt.Sprintf("  官网: %s\n", p.Website))
	}
	// 星级（酒店）
	if p.Stars != "" {
		sb.WriteString(fmt.Sprintf("  星级: %s\n", p.Stars))
	}
}

// BuildItineraryResult 把 LLM 返回的行程文本组装成完整响应。
func (s *ItineraryService) BuildItineraryResult(query string, opts *ItineraryOptions, travelResult *model.TravelResult, itineraryText string) *ItineraryResult {
	r := &ItineraryResult{
		Query:    query,
		LLM:      s.llmName,
		Days:     1,
		TripType: "自由行",
	}
	if opts != nil {
		if opts.Days > 0 {
			r.Days = opts.Days
		}
		if opts.TripType != "" {
			r.TripType = opts.TripType
		}
	}
	if travelResult != nil {
		r.Context.Primary = travelResult.Primary
		r.Context.Nearby = travelResult.Nearby
		r.Context.Weather = travelResult.Weather
		r.Context.Timezone = travelResult.Timezone
		r.Context.Wikipedia = travelResult.Wikipedia
	}
	r.Itinerary = itineraryText
	return r
}

// MarshalItineraryResult 将行程结果转为带格式的 Markdown 文件内容。
func MarshalItineraryResult(r *ItineraryResult) ([]byte, error) {
	var sb strings.Builder
	lang := llm.DetectLanguage(r.Query)

	if lang == "zh" {
		sb.WriteString(fmt.Sprintf("# 🗺️ %s 旅游攻略\n\n", r.Query))
		sb.WriteString(fmt.Sprintf("> 由 **%s** 驱动生成 · %d 日行程 · %s\n\n", r.LLM, r.Days, r.TripType))
	} else {
		sb.WriteString(fmt.Sprintf("# 🗺️ Travel Guide: %s\n\n", r.Query))
		sb.WriteString(fmt.Sprintf("> Powered by **%s** · %d day(s) · %s\n\n", r.LLM, r.Days, r.TripType))
	}

	// 目标景点
	if r.Context.Primary != nil {
		if lang == "zh" {
			sb.WriteString("## 📍 目标景点\n\n")
			sb.WriteString(fmt.Sprintf("- **名称**: %s\n", r.Context.Primary.Name))
			if r.Context.Primary.NameZh != "" {
				sb.WriteString(fmt.Sprintf("- **中文名**: %s\n", r.Context.Primary.NameZh))
			}
			if r.Context.Primary.FullAddress != "" {
				sb.WriteString(fmt.Sprintf("- **地址**: %s\n", r.Context.Primary.FullAddress))
			}
			if r.Context.Primary.OpeningHours != "" {
				sb.WriteString(fmt.Sprintf("- **开放时间**: %s\n", r.Context.Primary.OpeningHours))
			}
			if r.Context.Primary.Fee != "" {
				sb.WriteString(fmt.Sprintf("- **门票**: %s\n", r.Context.Primary.Fee))
			}
			if r.Context.Primary.Website != "" {
				sb.WriteString(fmt.Sprintf("- **官网**: %s\n", r.Context.Primary.Website))
			}
		} else {
			sb.WriteString("## 📍 Destination\n\n")
			sb.WriteString(fmt.Sprintf("- **Name**: %s\n", r.Context.Primary.Name))
			if r.Context.Primary.FullAddress != "" {
				sb.WriteString(fmt.Sprintf("- **Address**: %s\n", r.Context.Primary.FullAddress))
			}
			if r.Context.Primary.OpeningHours != "" {
				sb.WriteString(fmt.Sprintf("- **Hours**: %s\n", r.Context.Primary.OpeningHours))
			}
			if r.Context.Primary.Fee != "" {
				sb.WriteString(fmt.Sprintf("- **Admission**: %s\n", r.Context.Primary.Fee))
			}
		}
		sb.WriteString("\n---\n\n")
	}

	// 天气
	if r.Context.Weather != nil {
		if lang == "zh" {
			sb.WriteString(fmt.Sprintf("## 🌤️ 当前天气\n\n%s，气温 **%.1f°C**（体感 %.1f°C），湿度 %d%%\n\n---\n\n",
				r.Context.Weather.Description, r.Context.Weather.Temperature,
				r.Context.Weather.FeelsLike, r.Context.Weather.Humidity))
		} else {
			sb.WriteString(fmt.Sprintf("## 🌤️ Current Weather\n\n%s, **%.1f°C** (feels like %.1f°C), humidity %d%%\n\n---\n\n",
				r.Context.Weather.Description, r.Context.Weather.Temperature,
				r.Context.Weather.FeelsLike, r.Context.Weather.Humidity))
		}
	}

	// 行程正文
	if lang == "zh" {
		sb.WriteString("## 📋 行程安排\n\n")
	} else {
		sb.WriteString("## 📋 Itinerary\n\n")
	}
	sb.WriteString(r.Itinerary)
	sb.WriteString("\n\n---\n\n")

	// 小贴士
	if len(r.Context.Primary.Name) > 0 && lang == "zh" {
		sb.WriteString("## 💡 实用 Tips\n\n")
		if r.Context.Primary.Phone != "" {
			sb.WriteString(fmt.Sprintf("- 📞 联系电话: %s\n", r.Context.Primary.Phone))
		}
		if r.Context.Primary.Wheelchair != "" {
			sb.WriteString(fmt.Sprintf("- ♿ 无障碍: %s\n", r.Context.Primary.Wheelchair))
		}
		if r.Context.Timezone != "" {
			sb.WriteString(fmt.Sprintf("- 🕐 当地时区: %s\n", r.Context.Timezone))
		}
		if r.Context.Wikipedia != nil && r.Context.Wikipedia.URL != "" {
			sb.WriteString(fmt.Sprintf("- 📖 维基百科: %s\n", r.Context.Wikipedia.URL))
		}
	}

	if lang == "zh" {
		sb.WriteString("\n> *本攻略由 AI 自动生成，数据来源于 OpenStreetMap / Wikidata / Wikipedia 等开源数据，仅供参考。*\n")
	} else {
		sb.WriteString("\n> *This itinerary is AI-generated. Data sourced from OpenStreetMap / Wikidata / Wikipedia. For reference only.*\n")
	}

	return []byte(sb.String()), nil
}

// MarshalJSON 将 ItineraryResult 序列化为 JSON（含元数据，不含 Markdown）。
func MarshalJSON(r *ItineraryResult) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
