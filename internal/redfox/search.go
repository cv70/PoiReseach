package redfox

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type searchArticleReq struct {
	Keyword  string `json:"keyword"`
	Offset   int    `json:"offset,omitempty"`
	SortType string `json:"sortType,omitempty"`
}

type searchArticleRawItem struct {
	UUID      string `json:"uuid,omitempty"`
	ArticleID string `json:"article_id,omitempty"`
	ID        string `json:"id,omitempty"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Desc      string `json:"desc,omitempty"`
	Cover     string `json:"cover,omitempty"`
	CoverURL  string `json:"cover_url,omitempty"`
	URL       string `json:"url,omitempty"`
	Link      string `json:"link,omitempty"`
	ShareURL  string `json:"share_url,omitempty"`

	Author   string `json:"author,omitempty"`
	AuthorID string `json:"author_id,omitempty"`
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Biz      string `json:"biz,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	SecUID   string `json:"sec_uid,omitempty"`

	PublishedAt string `json:"published_at,omitempty"`
	PublishTime string `json:"publish_time,omitempty"`
	CreateTime  int64  `json:"create_time,omitempty"`
	CTime       int64  `json:"ctime,omitempty"`

	ReadCount    int64 `json:"read_count,omitempty"`
	LikeCount    int64 `json:"like_count,omitempty"`
	LikeNum      int64 `json:"like_num,omitempty"`
	CommentCount int64 `json:"comment_count,omitempty"`
	CommentNum   int64 `json:"comment_num,omitempty"`
	ShareCount   int64 `json:"share_count,omitempty"`
	ShareNum     int64 `json:"share_num,omitempty"`
	CollectCount int64 `json:"collect_count,omitempty"`
	CollectNum   int64 `json:"collect_num,omitempty"`
	ForwardCount int64 `json:"forward_count,omitempty"`
	DiggCount    int64 `json:"digg_count,omitempty"`
}

type searchArticleRawResp struct {
	List    []searchArticleRawItem `json:"list"`
	Items   []searchArticleRawItem `json:"items"`
	Total   int                    `json:"total"`
	Offset  int                    `json:"offset"`
	HasMore *bool                  `json:"has_more,omitempty"`
}

type articleConverter func(searchArticleRawItem) Article

func (c *Client) searchArticle(ctx context.Context, path string, keyword string, offset int, sortType SortType, conv articleConverter) (*SearchArticleResult, error) {
	if keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}
	if sortType == "" {
		sortType = SortByHot
	}
	req := searchArticleReq{
		Keyword:  keyword,
		Offset:   offset,
		SortType: string(sortType),
	}

	cacheKey := fmt.Sprintf("%s:%s:%d:%s", path, keyword, offset, sortType)
	if cached, ok := c.getCache(cacheKey); ok {
		return cached, nil
	}

	var raw searchArticleRawResp
	if err := c.post(ctx, path, req, &raw); err != nil {
		return nil, err
	}
	items := raw.List
	if len(items) == 0 {
		items = raw.Items
	}
	result := &SearchArticleResult{
		Total:  raw.Total,
		Offset: raw.Offset,
		Items:  make([]Article, 0, len(items)),
	}
	if raw.HasMore != nil {
		result.HasMore = *raw.HasMore
	} else {
		result.HasMore = len(items) > 0 && raw.Total > offset+len(items)
	}
	for _, item := range items {
		result.Items = append(result.Items, conv(item))
	}

	c.setCache(cacheKey, result)
	return result, nil
}

func (c *Client) SearchWeChatArticle(ctx context.Context, keyword string, offset int, sortType SortType) (*SearchArticleResult, error) {
	return c.searchArticle(ctx, "/gzhData/searchArticle", keyword, offset, sortType, convertWeChatArticle)
}

func convertWeChatArticle(r searchArticleRawItem) Article {
	a := Article{
		ID:       firstNonEmpty(r.ArticleID, r.ID, r.UUID),
		UUID:     r.UUID,
		Title:    r.Title,
		Content:  firstNonEmpty(r.Content, r.Desc, r.Summary),
		Summary:  firstNonEmpty(r.Summary, r.Desc),
		CoverURL: firstNonEmpty(r.CoverURL, r.Cover),
		URL:      firstNonEmpty(r.URL, r.Link, r.ShareURL),
		Platform: PlatformWeChat,
		Author:   firstNonEmpty(r.Author, r.Account, r.Nickname),
		AuthorID: firstNonEmpty(r.AuthorID, r.Biz),

		ReadCount:    r.ReadCount,
		LikeCount:    firstNonZero(r.LikeCount, r.LikeNum),
		CommentCount: firstNonZero(r.CommentCount, r.CommentNum),
		ShareCount:   firstNonZero(r.ShareCount, r.ShareNum),
		CollectCount: firstNonZero(r.CollectCount, r.CollectNum),
	}
	a.PublishedAt = parseTime(r.PublishedAt, r.PublishTime, r.CreateTime, r.CTime)
	return a
}

func (c *Client) SearchDouyinArticle(ctx context.Context, keyword string, offset int, sortType SortType) (*SearchArticleResult, error) {
	return c.searchArticle(ctx, "/dyData/searchArticle", keyword, offset, sortType, convertDouyinArticle)
}

func convertDouyinArticle(r searchArticleRawItem) Article {
	a := Article{
		ID:       firstNonEmpty(r.ID, r.ArticleID, r.UUID),
		UUID:     r.UUID,
		Title:    r.Title,
		Content:  firstNonEmpty(r.Content, r.Desc, r.Summary),
		Summary:  firstNonEmpty(r.Summary, r.Desc),
		CoverURL: firstNonEmpty(r.CoverURL, r.Cover),
		URL:      firstNonEmpty(r.URL, r.Link, r.ShareURL),
		Platform: PlatformDouyin,
		Author:   firstNonEmpty(r.Author, r.Nickname, r.Account),
		AuthorID: firstNonEmpty(r.AuthorID, r.SecUID, r.UserID),

		LikeCount:    firstNonZero(r.DiggCount, r.LikeCount, r.LikeNum),
		CommentCount: firstNonZero(r.CommentCount, r.CommentNum),
		ShareCount:   firstNonZero(r.ShareCount, r.ShareNum),
		CollectCount: firstNonZero(r.CollectCount, r.CollectNum),
		ForwardCount: firstNonZero(r.ForwardCount),
	}
	a.PublishedAt = parseTime(r.PublishedAt, r.PublishTime, r.CreateTime, r.CTime)
	return a
}

func (c *Client) SearchXiaohongshuArticle(ctx context.Context, keyword string, offset int, sortType SortType) (*SearchArticleResult, error) {
	return c.searchArticle(ctx, "/xhsUser/searchArticle", keyword, offset, sortType, convertXiaohongshuArticle)
}

func convertXiaohongshuArticle(r searchArticleRawItem) Article {
	a := Article{
		ID:       firstNonEmpty(r.ID, r.UUID, r.ArticleID),
		UUID:     r.UUID,
		Title:    r.Title,
		Content:  firstNonEmpty(r.Content, r.Desc, r.Summary),
		Summary:  firstNonEmpty(r.Summary, r.Desc),
		CoverURL: firstNonEmpty(r.CoverURL, r.Cover),
		URL:      firstNonEmpty(r.URL, r.Link, r.ShareURL),
		Platform: PlatformXiaohongshu,
		Author:   firstNonEmpty(r.Author, r.Nickname, r.Account),
		AuthorID: firstNonEmpty(r.AuthorID, r.UserID),

		LikeCount:    firstNonZero(r.LikeCount, r.LikeNum),
		CommentCount: firstNonZero(r.CommentCount, r.CommentNum),
		ShareCount:   firstNonZero(r.ShareCount, r.ShareNum),
		CollectCount: firstNonZero(r.CollectCount, r.CollectNum),
	}
	a.PublishedAt = parseTime(r.PublishedAt, r.PublishTime, r.CreateTime, r.CTime)
	return a
}

func parseTime(s1, s2 string, ts1, ts2 int64) time.Time {
	for _, s := range []string{s1, s2} {
		if s == "" {
			continue
		}
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	for _, ts := range []int64{ts1, ts2} {
		if ts > 0 {
			if ts > 1e12 {
				return time.UnixMilli(ts)
			}
			return time.Unix(ts, 0)
		}
	}
	return time.Time{}
}

func firstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstNonZero(nums ...int64) int64 {
	for _, n := range nums {
		if n != 0 {
			return n
		}
	}
	return 0
}

type cacheEntry struct {
	value     *SearchArticleResult
	expiresAt time.Time
}

var (
	cacheMu    sync.RWMutex
	cacheStore = make(map[string]cacheEntry)
	cacheTTL   = 5 * time.Minute
)

func (c *Client) getCache(key string) (*SearchArticleResult, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	entry, ok := cacheStore[cacheHash(key)]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (c *Client) setCache(key string, value *SearchArticleResult) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheStore[cacheHash(key)] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(cacheTTL),
	}
}

func cacheHash(key string) string {
	h := md5.Sum([]byte(key))
	return hex.EncodeToString(h[:])
}

func SetCacheTTL(ttl time.Duration) {
	if ttl > 0 {
		cacheMu.Lock()
		cacheTTL = ttl
		cacheMu.Unlock()
	}
}

func ClearCache() {
	cacheMu.Lock()
	cacheStore = make(map[string]cacheEntry)
	cacheMu.Unlock()
}

func CacheStats() (count int) {
	cacheMu.RLock()
	count = len(cacheStore)
	cacheMu.RUnlock()
	return
}

func marshalSize(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}
