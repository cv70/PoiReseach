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
	"poi-research/internal/service"
)

func main() {
	addr := flag.String("addr", getEnv("POI_HTTP_ADDR", ":8080"), "HTTP listen address")
	userAgent := flag.String("ua", getEnv("POI_USER_AGENT", "poi-research/2.0 (+local)"), "User-Agent for upstream APIs")
	llmProvider := flag.String("llm-provider", getEnv("LLM_PROVIDER", ""), "LLM provider: openai|claude|ollama|openai-compat (default: auto-detect)")
	llmModel := flag.String("llm-model", getEnv("LLM_MODEL", ""), "LLM model name (overrides default for provider)")
	flag.Parse()

	// 构建 LLM 实例（失败不影响主服务启动）
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

	svc := service.NewResearchService(*userAgent)
	var itSvc *service.ItineraryService
	if llmInst != nil {
		itSvc, _ = service.NewItineraryService(llmInst)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status":    "ok",
			"providers": svc.Providers(),
			"llm":       llmName,
			"time":      time.Now().Format(time.RFC3339),
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":         "POI Multi-Source Travel Research + AI Itinerary",
			"version":      "2.1",
			"poi_sources":  svc.Providers(),
			"llm_provider": llmName,
			"endpoints": map[string]string{
				"GET /health":                             "健康检查",
				"GET /api/search?q=...&limit=5":           "单源搜索（Nominatim）",
				"GET /api/search/multi?q=...&limit=3":     "多源并发搜索",
				"GET /api/travel?q=...":                  "旅游攻略：景点+周边+天气+维基百科",
				"GET /api/itinerary?q=...&days=1":         "AI 行程推荐（需配置 LLM）",
				"GET /api/research?q=...":                "旧版扁平结构（兼容）",
			},
			"llm_env_vars": map[string]string{
				"LLM_PROVIDER":            "openai|claude|ollama|openai-compat",
				"OPENAI_API_KEY":          "OpenAI API Key",
				"OPENAI_MODEL":            "模型名，如 gpt-4o-mini",
				"ANTHROPIC_API_KEY":       "Anthropic API Key",
				"ANTHROPIC_MODEL":         "模型名，如 claude-3-5-sonnet-20241022",
				"SILICONFLOW_API_KEY":     "硅基流动 API Key（openai-compat）",
				"OPENAI_COMPAT_BASE_URL":  "OpenAI 兼容端点 URL",
				"OPENAI_COMPAT_MODEL":     "兼容模型名",
				"OLLAMA_BASE_URL":         "本地 Ollama 地址，http://localhost:11434/v1",
				"OLLAMA_MODEL":            "Ollama 模型名，如 qwen2.5",
			},
			"examples": []string{
				"/api/travel?q=Eiffel+Tower",
				"/api/travel?q=故宫",
				"/api/search/multi?q=Kyoto+Fushimi+Inari",
				"/api/itinerary?q=京都&days=2&trip_type=情侣",
			},
		})
	})

	// 单源搜索
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// 多源搜索
	mux.HandleFunc("GET /api/search/multi", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// AI 行程推荐（新增）
	mux.HandleFunc("GET /api/itinerary", func(w http.ResponseWriter, r *http.Request) {
		if itSvc == nil {
			writeError(w, http.StatusServiceUnavailable, "LLM not configured: set OPENAI_API_KEY / ANTHROPIC_API_KEY / OLLAMA_BASE_URL")
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}

		// 解析偏好参数
		opts := &service.ItineraryOptions{
			TripType:     r.URL.Query().Get("trip_type"),
			Budget:       r.URL.Query().Get("budget"),
			Language:     r.URL.Query().Get("lang"),
			IncludePhoto: r.URL.Query().Get("photo") == "1",
		}
		if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
			opts.Days = n
		}

		// 生成超时链：旅游攻略最多 90s，LLM 最多 120s
		ctx, cancel := context.WithTimeout(r.Context(), 200*time.Second)
		defer cancel()

		// 1) 先拿景点数据
		travelResult, err := svc.TravelResearch(ctx, q)
		if err != nil {
			log.Printf("travel research error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// 2) LLM 生成行程
		itineraryText, err := itSvc.GenerateItinerary(ctx, travelResult, opts)
		if err != nil {
			log.Printf("itinerary generation error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		result := itSvc.BuildItineraryResult(q, opts, travelResult, itineraryText)

		// 支持返回格式：json（默认）或 markdown
		format := r.URL.Query().Get("format")
		if format == "markdown" || format == "md" {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			md, _ := service.MarshalItineraryResult(result)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, string(md))
			return
		}

		writeJSON(w, http.StatusOK, result)
	})

	// 旅游攻略
	mux.HandleFunc("GET /api/travel", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// 旧版兼容
	mux.HandleFunc("GET /api/research", func(w http.ResponseWriter, r *http.Request) {
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
	})

	handler := withCORS(withLogging(mux))

	server := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	go func() {
		log.Printf("POI travel research v2.1 listening on http://%s", *addr)
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
