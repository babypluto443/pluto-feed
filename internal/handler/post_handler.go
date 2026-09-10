package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// PostHandler 帖子接口：发布 / 详情 / 删除。
type PostHandler struct {
	svc *service.PostService
}

func NewPostHandler(svc *service.PostService) *PostHandler {
	return &PostHandler{svc: svc}
}

type createPostReq struct {
	ImageURLs []string `json:"image_urls"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
}

// POST /api/v1/posts（受保护）
func (h *PostHandler) Create(c *gin.Context) {
	uid, ok := middleware.UIDFrom(c.Request.Context())
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrUnauthorized)
		return
	}

	var req createPostReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}

	view, err := h.svc.Create(c.Request.Context(), uid, service.CreateInput{
		ImageURLs: req.ImageURLs,
		Content:   req.Content,
		Tags:      req.Tags,
	})
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, view)
}

// GET /api/v1/posts/:id（公开；OptionalAuth 提供登录者视角）
func (h *PostHandler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	viewerUID, _ := middleware.UIDFrom(c.Request.Context())
	view, err := h.svc.Get(c.Request.Context(), viewerUID, id)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, view)
}

// DELETE /api/v1/posts/:id（受保护，仅作者本人）
func (h *PostHandler) Delete(c *gin.Context) {
	uid, ok := middleware.UIDFrom(c.Request.Context())
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrUnauthorized)
		return
	}
	id, ok := pathID(c)
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), uid, id); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

// pathID 解析路径参数 :id（Gin 的 c.Param；net/http 时代是 r.PathValue）。
func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
