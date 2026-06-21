package provider

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"poi-research/internal/model"
)

// PlaceProvider 描述一个"点位搜索"数据源。
// 所有可插拔数据源都必须实现它。
type PlaceProvider interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]model.Place, error)
}

// Options 控制聚合搜索行为。
type Options struct {
	LimitPerProvider int // 每个源最多返回几条（<=0 用默认 5）
	KeepSource       bool // 是否在结果中保留 source 字段（默认 true）
}

// DefaultLimit 当 Options 未明确限定时使用。
const DefaultLimit = 5

// SearchAll 并发调用所有 providers，按 "名字 + 近似坐标" 去重，按 importance 倒序返回。
// 某个 provider 失败不影响整体，只记 log。
func SearchAll(ctx context.Context, query string, providers []PlaceProvider, opts *Options) []model.Place {
	if opts == nil {
		opts = &Options{}
	}
	limit := opts.LimitPerProvider
	if limit <= 0 {
		limit = DefaultLimit
	}

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		all  []model.Place
	)

	for _, p := range providers {
		p := p
		if p == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := p.Search(ctx, query, limit)
			if err != nil || len(results) == 0 {
				return
			}
			sourceName := p.Name()
			for i := range results {
				if results[i].Source == "" {
					results[i].Source = sourceName
				}
			}
			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// 去重：用 (name + 坐标桶) 作为 key
	type key struct {
		name string
		lat  int
		lon  int
	}
	seen := make(map[key]struct{}, len(all))
	merged := make([]model.Place, 0, len(all))
	for _, p := range all {
		lat, lon := approxLatLon(p.Lat, p.Lon)
		k := key{
			name: strings.ToLower(strings.TrimSpace(pickName(p))),
			lat:  lat,
			lon:  lon,
		}
		if _, ok := seen[k]; ok {
			// 已存在：保留 importance 更高的那一个
			continue
		}
		seen[k] = struct{}{}
		merged = append(merged, p)
	}

	// 排序：importance 倒序（0 排后面）
	sort.SliceStable(merged, func(i, j int) bool {
		ii, jj := merged[i].Importance, merged[j].Importance
		if math.Abs(ii-jj) > 1e-9 {
			return ii > jj
		}
		// importance 相等时，优先 Nominatim（它最权威）
		if merged[i].Source == "nominatim" {
			return true
		}
		if merged[j].Source == "nominatim" {
			return false
		}
		return strings.Compare(merged[i].DisplayName, merged[j].DisplayName) < 0
	})

	return merged
}

// PickBest 返回"最合适"的一条结果（当前实现即取排序后第一条）。
func PickBest(places []model.Place) (model.Place, bool) {
	if len(places) == 0 {
		return model.Place{}, false
	}
	return places[0], true
}

// ---------------------- 工具函数 ----------------------

func pickName(p model.Place) string {
	if p.Tags != nil {
		if v, ok := p.Tags["name:zh"]; ok && v != "" {
			return v
		}
	}
	if head := strings.TrimSpace(extractHead(p.DisplayName)); head != "" {
		return head
	}
	return p.DisplayName
}

func extractHead(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, sep := range []string{"，", ", "} {
		if idx := strings.Index(s, sep); idx >= 0 {
			return strings.TrimSpace(s[:idx])
		}
	}
	if idx := strings.Index(s, ","); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

// approxLatLon 把经纬度降到 0.01 度（~1km）桶里，做近似去重
func approxLatLon(latStr, lonStr string) (int, int) {
	lat, _ := strconv.ParseFloat(latStr, 64)
	lon, _ := strconv.ParseFloat(lonStr, 64)
	return int(lat * 100), int(lon * 100)
}
