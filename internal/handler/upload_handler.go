package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/service"
)

// UploadHandler 图片上传接口（受保护：只有登录用户能传图）。
type UploadHandler struct {
	svc *service.UploadService
}

func NewUploadHandler(svc *service.UploadService) *UploadHandler {
	return &UploadHandler{svc: svc}
}

// POST /api/v1/upload
// multipart 表单，字段名 file。
func (h *UploadHandler) Upload(c *gin.Context) {
	// MaxBytesReader：从传输层就掐断超大请求，不给攻击者灌 1GB 流量的机会
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxUploadSize+512)

	fh, err := c.FormFile("file")
	if err != nil {
		apierror.Fail(c.Writer, apierror.New(400, 40015, "请以 multipart/form-data 上传，字段名 file"))
		return
	}

	url, err := h.svc.Save(fh)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, map[string]string{"url": url})
}
