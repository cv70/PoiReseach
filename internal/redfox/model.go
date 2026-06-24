package redfox

import "time"

type Platform string

const (
	PlatformWeChat  Platform = "wechat"
	PlatformDouyin  Platform = "douyin"
	PlatformXiaohongshu Platform = "xiaohongshu"
)

type SortType string

const (
	SortByDefault   SortType = "_0"
	SortByTime      SortType = "_1"
	SortByReadCount SortType = "_2"
	SortByLikeCount SortType = "_3"
	SortByHot       SortType = "_4"
)

type Article struct {
	ID          string    `json:"id"`
	UUID        string    `json:"uuid,omitempty"`
	Title       string    `json:"title"`
	Author      string    `json:"author,omitempty"`
	AuthorID    string    `json:"author_id,omitempty"`
	Content     string    `json:"content,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	URL         string    `json:"url,omitempty"`
	Platform    Platform  `json:"platform"`
	PublishedAt time.Time `json:"published_at,omitempty"`

	ReadCount    int64 `json:"read_count,omitempty"`
	LikeCount    int64 `json:"like_count,omitempty"`
	CommentCount int64 `json:"comment_count,omitempty"`
	ShareCount   int64 `json:"share_count,omitempty"`
	CollectCount int64 `json:"collect_count,omitempty"`
	ForwardCount int64 `json:"forward_count,omitempty"`
}

type Account struct {
	ID         string   `json:"id"`
	UID        string   `json:"uid,omitempty"`
	Name       string   `json:"name"`
	AvatarURL  string   `json:"avatar_url,omitempty"`
	Platform   Platform `json:"platform"`
	Signature  string   `json:"signature,omitempty"`
	FollowerCount int64 `json:"follower_count,omitempty"`
	FollowingCount int64 `json:"following_count,omitempty"`
}

type SearchArticleResult struct {
	Total   int       `json:"total"`
	Offset  int       `json:"offset"`
	HasMore bool      `json:"has_more"`
	Items   []Article `json:"items"`
}

type SearchAccountResult struct {
	Total   int       `json:"total"`
	Items   []Account `json:"items"`
}
