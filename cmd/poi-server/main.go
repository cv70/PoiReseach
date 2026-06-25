package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"poi-research/internal/llm"
	"poi-research/internal/redfox"
	"poi-research/internal/service"
)

func main() {
	addr := flag.String("addr", getEnv("POI_HTTP_ADDR", ":8080"), "HTTP listen address")
	userAgent := flag.String("ua", getEnv("POI_USER_AGENT", "poi-research/2.0 (+local)"), "User-Agent for upstream APIs")
	llmProvider := flag.String("llm-provider", getEnv("LLM_PROVIDER", ""), "LLM provider: openai|claude|ollama|openai-compat (default: auto-detect)")
	llmModel := flag.String("llm-model", getEnv("LLM_MODEL", ""), "LLM model name (overrides default for provider)")
	flag.Parse()

	var llmInst llm.LLM
	var llmName string
	llmConfig := &llm.Config{Provider: *llmProvider}
	if *llmModel != "" {
		llmConfig.Model = *llmModel
	}
	llmConfig.FromEnv()
	llmInst, llmName = llm.BestEffort()
	if llmInst == nil {
		log.Println("[WARN] No LLM configured: set OPENAI_API_KEY / ANTHROPIC_API_KEY / SILICONFLOW_API_KEY / OLLAMA_BASE_URL")
		log.Println("[WARN] AI itinerary feature (/api/itinerary) will be unavailable until LLM is configured")
	} else {
		log.Printf("LLM enabled: provider=%s, name=%s", llmName, llmInst.Name())
	}

	var redfoxClient *redfox.Client
	if rf, err := redfox.NewClientFromEnv(); err == nil {
		redfoxClient = rf
		log.Println("RedFox enabled: wechat/douyin/xiaohongshu search")
	} else {
		log.Printf("[INFO] RedFox not configured (%v): set REDFOX_API_KEY to enable social media search", err)
	}

	svc := service.NewResearchService(*userAgent)
	var itSvc *service.ItineraryService
	if llmInst != nil {
		itSvc, _ = service.NewItineraryService(llmInst)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handleHealth(svc, llmName, redfoxClient))
	mux.HandleFunc("GET /", handleIndex(svc, llmName, redfoxClient))

	mux.HandleFunc("GET /api/search", handleSearch(svc))
	mux.HandleFunc("GET /api/search/multi", handleMultiSearch(svc))
	mux.HandleFunc("GET /api/travel", handleTravel(svc))
	mux.HandleFunc("GET /api/itinerary", handleItinerary(itSvc, svc))
	mux.HandleFunc("GET /api/research", handleResearch(svc))

	if redfoxClient != nil {
		mux.HandleFunc("GET /api/redfox/search", handleRedfoxSearch(redfoxClient))
		mux.HandleFunc("GET /api/redfox/search/multi", handleRedfoxMultiSearch(redfoxClient))
		mux.HandleFunc("GET /api/redfox/accounts", handleRedfoxAccounts(redfoxClient))
	}

	handler := withCORS(withLogging(mux))

	server := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	go func() {
		log.Printf("POI travel research v2.2 listening on http://%s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received...")

	sdCtx, sdCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer sdCancel()
	if err := server.Shutdown(sdCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("graceful shutdown complete")
}

func handleHealth(svc *service.ResearchService, llmName string, rf *redfox.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status":    "ok",
			"providers": svc.Providers(),
			"llm":       llmName,
			"time":      time.Now().Format(time.RFC3339),
		}
		if rf != nil {
			resp["redfox"] = map[string]any{
				"enabled":     true,
				"cache_count": redfox.CacheStats(),
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleIndex(svc *service.ResearchService, llmName string, rf *redfox.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints := map[string]string{
			"GET /health":                          "健康检查",
			"GET /api/search?q=...&limit=5":        "单源搜索（Nominatim）",
			"GET /api/search/multi?q=...&limit=3":  "多源并发搜索",
			"GET /api/travel?q=...":               "旅游攻略：景点+周边+天气+维基百科",
			"GET /api/itinerary?q=...&days=1":      "AI 行程推荐（需配置 LLM）",
			"GET /api/research?q=...":             "旧版扁平结构（兼容）",
		}
		if rf != nil {
			endpoints["GET /api/redfox/search?q=...&platform=wechat|douyin|xiaohongshu"] = "单平台新媒体内容搜索"
			endpoints["GET /api/redfox/search/multi?q=...&limit=10"] = "多平台新媒体内容并发搜索"
			endpoints["GET /api/redfox/accounts?q=...&platform=wechat|douyin|xiaohongshu"] = "新媒体账号搜索"
		}

		resp := map[string]any{
			"name":         "POI Multi-Source Travel Research + AI Itinerary + Social Media Search",
			"version":      "2.2",
			"poi_sources":  svc.Providers(),
			"llm_provider": llmName,
			"endpoints":    endpoints,
			"llm_env_vars": map[string]string{
				"LLM_PROVIDER":           "openai|claude|ollama|openai-compat",
				"OPENAI_API_KEY":         "OpenAI API Key",
				"OPENAI_MODEL":           "模型名，如 gpt-4o-mini",
				"ANTHROPIC_API_KEY":      "Anthropic API Key",
				"ANTHROPIC_MODEL":        "模型名，如 claude-3-5-sonnet-20241022",
				"SILICONFLOW_API_KEY":    "硅基流动 API Key（openai-compat）",
				"OPENAI_COMPAT_BASE_URL": "OpenAI 兼容端点 URL",
				"OPENAI_COMPAT_MODEL":    "兼容模型名",
				"OLLAMA_BASE_URL":        "本地 Ollama 地址，http://localhost:11434/v1",
				"OLLAMA_MODEL":           "Ollama 模型名，如 qwen2.5",
			},
			"examples": []string{
				"/api/travel?q=Eiffel+Tower",
				"/api/travel?q=故宫",
				"/api/search/multi?q=Kyoto+Fushimi+Inari",
				"/api/itinerary?q=京都&days=2&trip_type=情侣",
			},
		}
		if rf != nil {
			resp["redfox_env_vars"] = map[string]string{
				"REDFOX_API_KEY": "红狐数据 API Key（用于公众号/抖音/小红书搜索）",
				"REDFOX_BASE_URL": "红狐数据 API 自定义地址（可选）",
			}
			resp["redfox_examples"] = []string{
				"/api/redfox/search/multi?q=AI智能体",
				"/api/redfox/search?q=故宫&platform=wechat&sort=hot",
				"/api/redfox/accounts?q=人民日报&platform=wechat",
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleSearch(svc *service.ResearchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		limit := 5
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
			limit = n
		}
		result, err := svc.SearchOnly(r.Context(), q, limit)
		if err != nil {
			log.Printf("search error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"query": q, "count": len(result), "results": result})
	}
}

func handleMultiSearch(svc *service.ResearchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		limit := 3
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
			limit = n
		}
		result := svc.MultiSearch(r.Context(), q, limit)
		writeJSON(w, http.StatusOK, map[string]any{
			"query": q, "count": len(result), "data_sources": svc.Providers(), "results": result,
		})
	}
}

func handleTravel(svc *service.ResearchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
		defer cancel()
		result, err := svc.TravelResearch(ctx, q)
		if err != nil {
			log.Printf("travel error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"query": q, "data_sources": svc.Providers(), "result": result})
	}
}

func handleItinerary(itSvc *service.ItineraryService, svc *service.ResearchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if itSvc == nil {
			writeError(w, http.StatusServiceUnavailable, "LLM not configured: set OPENAI_API_KEY / ANTHROPIC_API_KEY / OLLAMA_BASE_URL")
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}

		opts := &service.ItineraryOptions{
			TripType:     r.URL.Query().Get("trip_type"),
			Budget:       r.URL.Query().Get("budget"),
			Language:     r.URL.Query().Get("lang"),
			IncludePhoto: r.URL.Query().Get("photo") == "1",
		}
		if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
			opts.Days = n
		}

		ctx, cancel := context.WithTimeout(r.Context(), 200*time.Second)
		defer cancel()

		travelResult, err := svc.TravelResearch(ctx, q)
		if err != nil {
			log.Printf("travel research error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		itineraryText, err := itSvc.GenerateItinerary(ctx, travelResult, opts)
		if err != nil {
			log.Printf("itinerary generation error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		result := itSvc.BuildItineraryResult(q, opts, travelResult, itineraryText)

		format := r.URL.Query().Get("format")
		if format == "markdown" || format == "md" {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			md, _ := service.MarshalItineraryResult(result)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, string(md))
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleResearch(svc *service.ResearchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		result, err := svc.DeepResearch(ctx, q)
		if err != nil {
			log.Printf("research error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleRedfoxSearch(client *redfox.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
		if platform == "" {
			platform = "wechat"
		}
		offset := 0
		if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		sort := strings.ToLower(r.URL.Query().Get("sort"))
		var sortType redfox.SortType
		switch sort {
		case "time", "_1":
			sortType = redfox.SortByTime
		case "read", "_2":
			sortType = redfox.SortByReadCount
		case "like", "_3":
			sortType = redfox.SortByLikeCount
		case "hot", "_4":
			sortType = redfox.SortByHot
		default:
			sortType = redfox.SortByDefault
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var result *redfox.SearchArticleResult
		var err error

		switch platform {
		case "wechat", "wx", "gzh", "weixin":
			result, err = client.SearchWeChatArticle(ctx, q, offset, sortType)
		case "douyin", "dy", "tiktok":
			result, err = client.SearchDouyinArticle(ctx, q, offset, sortType)
		case "xiaohongshu", "xhs", "red", "redbook":
			result, err = client.SearchXiaohongshuArticle(ctx, q, offset, sortType)
		default:
			writeError(w, http.StatusBadRequest, "invalid platform: use wechat|douyin|xiaohongshu")
			return
		}

		if err != nil {
			log.Printf("redfox search error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query":    q,
			"platform": platform,
			"total":    result.Total,
			"offset":   result.Offset,
			"has_more": result.HasMore,
			"count":    len(result.Items),
			"items":    result.Items,
		})
	}
}

func handleRedfoxMultiSearch(client *redfox.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		limit := 10
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
			limit = n
		}

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		result := client.MultiSearch(ctx, q, limit)
		writeJSON(w, http.StatusOK, result)
	}
}

func handleRedfoxAccounts(client *redfox.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
		if platform == "" {
			platform = "wechat"
		}
		offset := 0
		if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
			offset = n
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		var result *redfox.SearchAccountResult
		var err error

		switch platform {
		case "wechat", "wx", "gzh", "weixin":
			result, err = client.SearchWeChatAccount(ctx, q, offset)
		case "douyin", "dy", "tiktok":
			result, err = client.SearchDouyinAccount(ctx, q, offset)
		case "xiaohongshu", "xhs", "red", "redbook":
			result, err = client.SearchXiaohongshuAccount(ctx, q, offset)
		default:
			writeError(w, http.StatusBadRequest, "invalid platform: use wechat|douyin|xiaohongshu")
			return
		}

		if err != nil {
			log.Printf("redfox account search error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query":    q,
			"platform": platform,
			"total":    result.Total,
			"count":    len(result.Items),
			"items":    result.Items,
		})
	}
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
