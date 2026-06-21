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
	userAgent := flag.String("ua", getEnv("POI_USER_AGENT", "poi-research/2.0 (+local multi-source travel tool)"), "User-Agent for upstream APIs")
	flag.Parse()

	svc := service.NewResearchService(*userAgent)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"providers": svc.Providers(),
			"time":      time.Now().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":        "POI Multi-Source Travel Research",
			"version":     "2.0",
			"data_sources": svc.Providers(),
			"endpoints": map[string]string{
				"GET /health":                         "健康检查 + 当前 provider 列表",
				"GET /api/search?q=NAME&limit=5":      "单源快速搜索（默认使用 Nominatim）",
				"GET /api/search/multi?q=NAME&limit=3": "多源并发搜索（Nominatim + Photon + Wikidata）",
				"GET /api/travel?q=NAME":              "旅游攻略：景点详情 + 分类周边 + 维基百科 + 天气 + 小贴士",
				"GET /api/research?q=NAME":            "旧版扁平结构，与老客户端兼容",
			},
			"examples": []string{
				"/api/travel?q=Eiffel+Tower",
				"/api/travel?q=故宫",
				"/api/travel?q=Kyoto+Fushimi+Inari",
				"/api/search/multi?q=Times+Square&limit=3",
			},
		})
	})

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
			"query":   q,
			"count":   len(result),
			"source":  "nominatim (single)",
			"results": result,
		})
	})

	mux.HandleFunc("GET /api/search/multi", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}
		limitPerSource := 3
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limitPerSource = n
			}
		}

		result := svc.MultiSearch(r.Context(), q, limitPerSource)
		writeJSON(w, http.StatusOK, map[string]any{
			"query":        q,
			"count":        len(result),
			"data_sources": svc.Providers(),
			"results":      result,
		})
	})

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

	mux.HandleFunc("GET /api/travel", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required. Example: /api/travel?q=Eiffel+Tower")
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
		writeJSON(w, http.StatusOK, map[string]any{
			"query":        q,
			"data_sources": svc.Providers(),
			"result":       result,
		})
	})

	handler := withCORS(withLogging(mux))

	server := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	go func() {
		log.Printf("POI travel research v2 listening on http://%s (providers=%v)", *addr, svc.Providers())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutdown signal received, draining requests...")

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
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
