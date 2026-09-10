package service

import (
	"time"

	"pluto_feed/internal/model"
)

// AuthorView 帖子作者在 API 响应里的形态（只暴露安全字段）。
type AuthorView struct {
	ID        int64  `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

// PostView 帖子在 API 响应里的形态：帖子数据 + 作者信息拼装。
// service 层负责把 model（表结构）加工成 view（接口契约），
// handler 不接触 model，model 也不感知 JSON 接口长什么样。
type PostView struct {
	ID           int64            `json:"id"`
	UserID       int64            `json:"user_id"`
	Author       AuthorView       `json:"author"`
	ImageURLs    model.StringList `json:"image_urls"`
	Content      string           `json:"content"`
	Tags         model.StringList `json:"tags"`
	LikeCount    int64            `json:"like_count"`
	CommentCount int64            `json:"comment_count"`
	Liked        bool             `json:"liked"` // 当前用户是否已点赞（未登录恒为 false）
	CreatedAt    time.Time        `json:"created_at"`
}

// NewPostView 把帖子 + 作者拼成视图。作者可能已被删号（u=nil），此时给占位。
func NewPostView(p *model.Post, u *model.User) PostView { // 不含 liked：由调用方按 viewer 填充
	author := AuthorView{ID: p.UserID, Nickname: "已注销用户"}
	if u != nil {
		author = AuthorView{ID: u.ID, Nickname: u.Nickname, AvatarURL: u.AvatarURL}
	}
	return PostView{
		ID:           p.ID,
		UserID:       p.UserID,
		Author:       author,
		ImageURLs:    p.ImageURLs,
		Content:      p.Content,
		Tags:         p.Tags,
		LikeCount:    p.LikeCount,
		CommentCount: p.CommentCount,
		CreatedAt:    p.CreatedAt,
	}
}

// CommentView 评论在 API 响应里的形态。
type CommentView struct {
	ID        int64      `json:"id"`
	PostID    int64      `json:"post_id"`
	Author    AuthorView `json:"author"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
}

func NewCommentView(c *model.Comment, u *model.User) CommentView {
	author := AuthorView{ID: c.UserID, Nickname: "已注销用户"}
	if u != nil {
		author = AuthorView{ID: u.ID, Nickname: u.Nickname, AvatarURL: u.AvatarURL}
	}
	return CommentView{
		ID:        c.ID,
		PostID:    c.PostID,
		Author:    author,
		Content:   c.Content,
		CreatedAt: c.CreatedAt,
	}
}

// CommentPage 一页评论（翻页结构与 FeedPage 一致）。
type CommentPage struct {
	Items      []CommentView `json:"items"`
	NextCursor string        `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}
