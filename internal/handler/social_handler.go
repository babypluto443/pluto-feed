package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// SocialHandler 关注关系接口。
type SocialHandler struct {
	svc *service.SocialService
}

func NewSocialHandler(svc *service.SocialService) *SocialHandler {
	return &SocialHandler{svc: svc}
}

// POST /api/v1/users/:id/follow（受保护）
func (h *SocialHandler) Follow(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	targetID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Follow(c.Request.Context(), uid, targetID); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

// DELETE /api/v1/users/:id/follow（受保护）
func (h *SocialHandler) Unfollow(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	targetID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Unfollow(c.Request.Context(), uid, targetID); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

// GET /api/v1/users/:id/following（公开，游标分页）
func (h *SocialHandler) ListFollowing(c *gin.Context) {
	h.list(c, true)
}

// GET /api/v1/users/:id/followers（公开，游标分页）
func (h *SocialHandler) ListFollowers(c *gin.Context) {
	h.list(c, false)
}

func (h *SocialHandler) list(c *gin.Context, following bool) {
	uid, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	cursor := c.Query("cursor")

	var page *service.SocialListPage
	var svcErr error
	if following {
		page, svcErr = h.svc.ListFollowing(c.Request.Context(), uid, cursor, limit)
	} else {
		page, svcErr = h.svc.ListFollowers(c.Request.Context(), uid, cursor, limit)
	}
	if svcErr != nil {
		apierror.Fail(c.Writer, svcErr)
		return
	}
	apierror.OK(c.Writer, page)
}

// ProfileHandler 个人主页聚合接口。
type ProfileHandler struct {
	svc *service.ProfileService
}

func NewProfileHandler(svc *service.ProfileService) *ProfileHandler {
	return &ProfileHandler{svc: svc}
}

// GET /api/v1/users/:id/profile（公开）
func (h *ProfileHandler) Get(c *gin.Context) {
	targetID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	// viewerID：未登录为 0。本期未使用，M3 加 is_following 时启用。
	viewerID, _ := middleware.UIDFrom(c.Request.Context())

	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	view, err := h.svc.Get(c.Request.Context(), viewerID, targetID,
		c.Query("cursor"), limit)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, view)
}
