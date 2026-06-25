package redfox

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type searchAccountReq struct {
	Keyword string `json:"keyword"`
	Offset  int    `json:"offset,omitempty"`
}

type searchAccountRawItem struct {
	ID             string `json:"id,omitempty"`
	UID            string `json:"uid,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	SecUID         string `json:"sec_uid,omitempty"`
	Biz            string `json:"biz,omitempty"`
	Name           string `json:"name,omitempty"`
	Nickname       string `json:"nickname,omitempty"`
	Account        string `json:"account,omitempty"`
	Username       string `json:"username,omitempty"`
	AvatarURL      string `json:"avatar_url,omitempty"`
	Avatar         string `json:"avatar,omitempty"`
	HeadImgURL     string `json:"head_img_url,omitempty"`
	Signature      string `json:"signature,omitempty"`
	Desc           string `json:"desc,omitempty"`
	FollowerCount  int64  `json:"follower_count,omitempty"`
	FansCount      int64  `json:"fans_count,omitempty"`
	FollowingCount int64  `json:"following_count,omitempty"`
	Total          int    `json:"total,omitempty"`
}

type searchAccountRawResp struct {
	List  []searchAccountRawItem `json:"list"`
	Items []searchAccountRawItem `json:"items"`
	Total int                    `json:"total"`
}

type accountConverter func(searchAccountRawItem) Account

func (c *Client) searchAccount(ctx context.Context, path string, keyword string, offset int, conv accountConverter) (*SearchAccountResult, error) {
	if keyword == "" {
		return nil, fmt.Errorf("keyword is required")
	}
	req := searchAccountReq{Keyword: keyword, Offset: offset}
	var raw searchAccountRawResp
	if err := c.post(ctx, path, req, &raw); err != nil {
		return nil, err
	}
	items := raw.List
	if len(items) == 0 {
		items = raw.Items
	}
	result := &SearchAccountResult{
		Total: raw.Total,
		Items: make([]Account, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, conv(item))
	}
	return result, nil
}

func (c *Client) SearchWeChatAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	return c.searchAccount(ctx, "/gzhData/searchUser", keyword, offset, convertWeChatAccount)
}

func convertWeChatAccount(r searchAccountRawItem) Account {
	return Account{
		ID:             firstNonEmpty(r.Biz, r.ID, r.UID, r.UserID),
		UID:            firstNonEmpty(r.UID, r.Biz, r.UserID),
		Name:           firstNonEmpty(r.Name, r.Nickname, r.Account, r.Username),
		AvatarURL:      firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:       PlatformWeChat,
		Signature:      firstNonEmpty(r.Signature, r.Desc),
		FollowerCount:  firstNonZero(r.FollowerCount, r.FansCount),
		FollowingCount: firstNonZero(r.FollowingCount),
	}
}

func (c *Client) SearchDouyinAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	return c.searchAccount(ctx, "/dyData/searchUser", keyword, offset, convertDouyinAccount)
}

func convertDouyinAccount(r searchAccountRawItem) Account {
	return Account{
		ID:             firstNonEmpty(r.SecUID, r.UID, r.UserID, r.ID),
		UID:            firstNonEmpty(r.UID, r.SecUID, r.UserID),
		Name:           firstNonEmpty(r.Nickname, r.Name, r.Username, r.Account),
		AvatarURL:      firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:       PlatformDouyin,
		Signature:      firstNonEmpty(r.Signature, r.Desc),
		FollowerCount:  firstNonZero(r.FollowerCount, r.FansCount),
		FollowingCount: firstNonZero(r.FollowingCount),
	}
}

func (c *Client) SearchXiaohongshuAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	return c.searchAccount(ctx, "/xhsUser/searchUser", keyword, offset, convertXiaohongshuAccount)
}

func convertXiaohongshuAccount(r searchAccountRawItem) Account {
	return Account{
		ID:             firstNonEmpty(r.UserID, r.UID, r.ID),
		UID:            firstNonEmpty(r.UID, r.UserID),
		Name:           firstNonEmpty(r.Nickname, r.Name, r.Username, r.Account),
		AvatarURL:      firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:       PlatformXiaohongshu,
		Signature:      firstNonEmpty(r.Signature, r.Desc),
		FollowerCount:  firstNonZero(r.FollowerCount, r.FansCount),
		FollowingCount: firstNonZero(r.FollowingCount),
	}
}

type MultiSearchResult struct {
	Query       string              `json:"query"`
	WeChat      *SearchArticleResult `json:"wechat,omitempty"`
	Douyin      *SearchArticleResult `json:"douyin,omitempty"`
	Xiaohongshu *SearchArticleResult `json:"xiaohongshu,omitempty"`
	AllItems    []Article           `json:"all_items"`
	Errors      map[string]string   `json:"errors,omitempty"`
}

func (c *Client) MultiSearch(ctx context.Context, keyword string, limitPerPlatform int) *MultiSearchResult {
	if limitPerPlatform <= 0 {
		limitPerPlatform = 10
	}

	result := &MultiSearchResult{
		Query:    keyword,
		AllItems: make([]Article, 0),
		Errors:   make(map[string]string),
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	platforms := []struct {
		name   Platform
		search func(context.Context, string, int, SortType) (*SearchArticleResult, error)
	}{
		{PlatformWeChat, c.SearchWeChatArticle},
		{PlatformDouyin, c.SearchDouyinArticle},
		{PlatformXiaohongshu, c.SearchXiaohongshuArticle},
	}

	for _, p := range platforms {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := p.search(ctx, keyword, 0, SortByHot)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors[string(p.name)] = err.Error()
				return
			}
			if res == nil {
				return
			}
			switch p.name {
			case PlatformWeChat:
				result.WeChat = res
			case PlatformDouyin:
				result.Douyin = res
			case PlatformXiaohongshu:
				result.Xiaohongshu = res
			}
			items := res.Items
			if len(items) > limitPerPlatform {
				items = items[:limitPerPlatform]
			}
			result.AllItems = append(result.AllItems, items...)
		}()
	}

	wg.Wait()

	sort.Slice(result.AllItems, func(i, j int) bool {
		si := result.AllItems[i].LikeCount + result.AllItems[i].ReadCount + result.AllItems[i].ShareCount
		sj := result.AllItems[j].LikeCount + result.AllItems[j].ReadCount + result.AllItems[j].ShareCount
		return si > sj
	})

	return result
}
