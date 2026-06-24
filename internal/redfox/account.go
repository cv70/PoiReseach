package redfox

import (
	"context"
	"sync"
)

type searchAccountReq struct {
	Keyword string `json:"keyword"`
	Offset  int    `json:"offset,omitempty"`
}

type searchAccountRawItem struct {
	ID         string `json:"id,omitempty"`
	UID        string `json:"uid,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	SecUID     string `json:"sec_uid,omitempty"`
	Biz        string `json:"biz,omitempty"`
	Name       string `json:"name,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Account    string `json:"account,omitempty"`
	Username   string `json:"username,omitempty"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	HeadImgURL string `json:"head_img_url,omitempty"`
	Signature  string `json:"signature,omitempty"`
	Desc       string `json:"desc,omitempty"`
	FollowerCount  int64 `json:"follower_count,omitempty"`
	FansCount      int64 `json:"fans_count,omitempty"`
	FollowingCount int64 `json:"following_count,omitempty"`
	Total int `json:"total,omitempty"`
}

type searchAccountRawResp struct {
	List  []searchAccountRawItem `json:"list"`
	Items []searchAccountRawItem `json:"items"`
	Total int                    `json:"total"`
}

func (c *Client) SearchWeChatAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	req := searchAccountReq{Keyword: keyword, Offset: offset}
	var raw searchAccountRawResp
	if err := c.post(ctx, "/gzhData/searchUser", req, &raw); err != nil {
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
		result.Items = append(result.Items, convertWeChatAccount(item))
	}
	return result, nil
}

func convertWeChatAccount(r searchAccountRawItem) Account {
	return Account{
		ID:         firstNonEmpty(r.Biz, r.ID, r.UID, r.UserID),
		UID:        firstNonEmpty(r.UID, r.Biz, r.UserID),
		Name:       firstNonEmpty(r.Name, r.Nickname, r.Account, r.Username),
		AvatarURL:  firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:   PlatformWeChat,
		Signature:  firstNonEmpty(r.Signature, r.Desc),
		FollowerCount:  firstNonZero(r.FollowerCount, r.FansCount),
		FollowingCount: firstNonZero(r.FollowingCount),
	}
}

func (c *Client) SearchDouyinAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	req := searchAccountReq{Keyword: keyword, Offset: offset}
	var raw searchAccountRawResp
	if err := c.post(ctx, "/dyData/searchUser", req, &raw); err != nil {
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
		result.Items = append(result.Items, convertDouyinAccount(item))
	}
	return result, nil
}

func convertDouyinAccount(r searchAccountRawItem) Account {
	return Account{
		ID:         firstNonEmpty(r.SecUID, r.UID, r.UserID, r.ID),
		UID:        firstNonEmpty(r.UID, r.SecUID, r.UserID),
		Name:       firstNonEmpty(r.Nickname, r.Name, r.Username, r.Account),
		AvatarURL:  firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:   PlatformDouyin,
		Signature:  firstNonEmpty(r.Signature, r.Desc),
		FollowerCount:  firstNonZero(r.FollowerCount, r.FansCount),
		FollowingCount: firstNonZero(r.FollowingCount),
	}
}

func (c *Client) SearchXiaohongshuAccount(ctx context.Context, keyword string, offset int) (*SearchAccountResult, error) {
	req := searchAccountReq{Keyword: keyword, Offset: offset}
	var raw searchAccountRawResp
	if err := c.post(ctx, "/xhsUser/searchUser", req, &raw); err != nil {
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
		result.Items = append(result.Items, convertXiaohongshuAccount(item))
	}
	return result, nil
}

func convertXiaohongshuAccount(r searchAccountRawItem) Account {
	return Account{
		ID:         firstNonEmpty(r.UserID, r.UID, r.ID),
		UID:        firstNonEmpty(r.UID, r.UserID),
		Name:       firstNonEmpty(r.Nickname, r.Name, r.Username, r.Account),
		AvatarURL:  firstNonEmpty(r.AvatarURL, r.Avatar, r.HeadImgURL),
		Platform:   PlatformXiaohongshu,
		Signature:  firstNonEmpty(r.Signature, r.Desc),
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
}

func (c *Client) MultiSearch(ctx context.Context, keyword string, limitPerPlatform int) *MultiSearchResult {
	if limitPerPlatform <= 0 {
		limitPerPlatform = 10
	}

	result := &MultiSearchResult{
		Query:    keyword,
		AllItems: make([]Article, 0),
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
	)

	platforms := []struct {
		name  Platform
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
			if err != nil || res == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
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
	return result
}
