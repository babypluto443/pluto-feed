package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// InteractionHandler 互动接口：点赞 / 取消 / 评论 / 评论列表 / 删评论。
type InteractionHandler struct {
	svc *service.InteractionService
}

func NewInteractionHandler(svc *service.InteractionService) *InteractionHandler {
	return &InteractionHandler{svc: svc}
}

// mustUID 从 context 取登录用户；受保护路由里不可能缺失，防御性兜底。
func mustUID(c *gin.Context) (int64, bool) {
	uid, ok := middleware.UIDFrom(c.Request.Context())
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrUnauthorized)
	}
	return uid, ok
}

// POST /api/v1/posts/:id/like（受保护）
func (h *InteractionHandler) Like(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	postID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Like(c.Request.Context(), uid, postID); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

// DELETE /api/v1/posts/:id/like（受保护）
func (h *InteractionHandler) Unlike(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	postID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Unlike(c.Request.Context(), uid, postID); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

type createCommentReq struct {
	Content string `json:"content"`
}

// POST /api/v1/posts/:id/comments（受保护）
func (h *InteractionHandler) Comment(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	postID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	var req createCommentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	view, err := h.svc.Comment(c.Request.Context(), uid, postID, req.Content)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, view)
}

// GET /api/v1/posts/:id/comments（公开，游标分页）
func (h *InteractionHandler) ListComments(c *gin.Context) {
	postID, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 0
	}
	page, err := h.svc.ListComments(c.Request.Context(), postID, c.Query("cursor"), limit)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, page)
}

// DELETE /api/v1/comments/:id（受保护，仅评论作者本人）
func (h *InteractionHandler) DeleteComment(c *gin.Context) {
	uid, ok := mustUID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.DeleteComment(c.Request.Context(), uid, id); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}
