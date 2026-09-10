package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// FeedHandler Feed 流接口。
type FeedHandler struct {
	svc *service.FeedService
}

func NewFeedHandler(svc *service.FeedService) *FeedHandler {
	return &FeedHandler{svc: svc}
}

// GET /api/v1/feed?cursor=&limit=（公开，推荐流 = 全站时间倒序）
// cursor 上一页响应里的 next_cursor 原样传回；首页不传。
// limit 每页条数，默认 20，上限 50。
func (h *FeedHandler) List(c *gin.Context) {
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0 // 解析失败交给 service 用默认值
	}
	viewerUID, _ := middleware.UIDFrom(c.Request.Context())
	page, err := h.svc.List(c.Request.Context(), viewerUID, c.Query("cursor"), limit)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, page)
}

// GET /api/v1/feed/following（受保护，M2 关注流）
func (h *FeedHandler) ListFollowing(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	page, err := h.svc.ListFollowingFeed(c.Request.Context(), uid,
		c.Query("cursor"), limit)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, page)
}

// GET /api/v1/feed/hot（公开，M3 热榜）
func (h *FeedHandler) ListHot(c *gin.Context) {
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	viewerUID, _ := middleware.UIDFrom(c.Request.Context())
	page, err := h.svc.ListHot(c.Request.Context(), viewerUID, limit)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, page)
}
