// Package handler HTTP 处理层：只做三件事——绑参数、调 service、写响应。不写业务逻辑。
//
// M5 Gin 迁移说明（迁移映射表，见 09-M5-开发记录.md）：
//   http.HandlerFunc(w, r)      → gin.HandlerFunc(c)
//   bindJSON(r, &req)           → c.ShouldBindJSON(&req)
//   r.PathValue("id")           → c.Param("id")（路由 {id} → :id）
//   r.URL.Query().Get(k)        → c.Query(k)
//   apierror.OK(w, v)           → apierror.OK(c.Writer, v)   ← 响应格式单点不变
//   r.Context()                 → c.Request.Context()        ← uid 仍走 request context
package handler

import (
	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// AuthHandler 认证相关接口。
type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type registerReq struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

// POST /api/v1/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Register(c.Request.Context(), req.Nickname, req.Password); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

type loginReq struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

// POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	tp, err := h.svc.Login(c.Request.Context(), req.Nickname, req.Password)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, tp)
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

// POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	tp, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, tp)
}

type logoutReq struct {
	RefreshToken string `json:"refresh_token"`
}

// POST /api/v1/auth/logout（受保护）：白名单删除 refresh token，全端吊销。
func (h *AuthHandler) Logout(c *gin.Context) {
	var req logoutReq
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		apierror.Fail(c.Writer, apierror.ErrBadRequest)
		return
	}
	if err := h.svc.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, nil)
}

// GET /api/v1/me —— 受保护接口，演示认证中间件 + context 传值。
func (h *AuthHandler) Me(c *gin.Context) {
	uid, ok := middleware.UIDFrom(c.Request.Context())
	if !ok {
		apierror.Fail(c.Writer, apierror.ErrUnauthorized)
		return
	}
	u, err := h.svc.Me(c.Request.Context(), uid)
	if err != nil {
		apierror.Fail(c.Writer, err)
		return
	}
	apierror.OK(c.Writer, u)
}
