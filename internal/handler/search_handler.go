package handler

import (
	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// SearchHandler 全文搜索（O4）。
type SearchHandler struct {
	svc *service.PostService
}

func NewSearchHandler(svc *service.PostService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// GET /api/v1/search?q=（公开；OptionalAuth 提供点赞态视角）
func (h *SearchHandler) Search(c *gin.Context) {
	viewerUID, _ := middleware.UIDFrom(c.Request.Context())
	q := c.Query("q")
	if q == "" {
		apierror.OK(c.Writer, []service.PostView{})
		return
	}
	views, err := h.svc.Search(c.Request.Context(), viewerUID, q, 0)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, views)
}
