package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"poi-research/internal/service"
)

func main() {
	addr := flag.String("addr", getEnv("POI_HTTP_ADDR", ":8080"), "HTTP listen address")
	userAgent := flag.String("ua", getEnv("POI_USER_AGENT", "poi-research/1.0 (local travel research tool)"), "User-Agent for upstream APIs")
	flag.Parse()

	svc := service.NewResearchService(*userAgent)

	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().Format(time.RFC3339)})
	})

	// 根路径：接口说明
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"endpoints": map[string]string{
				"GET /health":                              "健康检查",
				"GET /api/search?q=NAME&limit=5":           "只调用 Nominatim 返回候选地点",
				"GET /api/research?q=NAME":                 "深度研究（旧扁平结构）",
				"GET /api/travel?q=NAME":                   "旅游攻略（推荐）— 景点详情 + 分类周边 + 维基百科 + 天气 + 小贴士",
			},
			"examples": []string{
				"/api/travel?q=Eiffel%20Tower",
				"/api/travel?q=故宫",
				"/api/travel?q=Kyoto%20Fushimi%20Inari",
				"/api/search?q=Times%20Square&limit=3",
			},
			"notes": "所有数据实时聚合自 Nominatim / Overpass / Wikipedia / Open-Meteo 等开源 API，不做本地存储。注意各上游有速率限制。",
		})
	})

	// 仅搜索（Nominatim）
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		limit := 5
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		result, err := svc.SearchOnly(r.Context(), q, limit)
		if err != nil {
			log.Printf("search error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query":  q,
			"count":  len(result),
			"places": result,
		})
	})

	// 深度研究（旧接口，保持兼容）
	mux.HandleFunc("GET /api/research", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		result, err := svc.DeepResearch(ctx, q)
		if err != nil {
			log.Printf("research error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	// 旅游攻略（新接口，推荐）
	mux.HandleFunc("GET /api/travel", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required. Example: /api/travel?q=Eiffel%20Tower")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()

		result, err := svc.TravelResearch(ctx, q)
		if err != nil {
			log.Printf("travel error: %v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	// CORS（对浏览器更友好）
	handler := withCORS(withLogging(mux))

	server := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 240 * time.Second,
	}

	go func() {
		log.Printf("POI travel research service listening on http://%s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received, draining requests...")

	sdCtx, sdCancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
