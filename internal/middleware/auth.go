package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/jwtutil"
)

// ctxKey 私有类型：防止其他包用同名 key 碰撞 context。
type ctxKey struct{}

// GinAuth 认证中间件（M5 Gin 版）：校验 Bearer access token，把 uid 塞进 request context。
//
// 设计要点（迁移时保住的关键决策）：uid 仍写入 **request context** 而非 gin.Context——
// 这样所有 handler 继续用 middleware.UIDFrom(r.Context()) 取值，
// handler 层与传输框架解耦（将来再换框架也不用动 service/handler 的取值代码）。
func GinAuth(jm *jwtutil.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		// 格式必须是 "Bearer <token>"
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			apierror.Fail(c.Writer, apierror.ErrUnauthorized)
			c.Abort()
			return
		}
		uid, err := jm.Parse(strings.TrimPrefix(header, prefix), jwtutil.TypAccess)
		if err != nil {
			apierror.Fail(c.Writer, apierror.ErrUnauthorized)
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), ctxKey{}, uid)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// UIDFrom 从 context 里取当前登录用户 id；未认证时 ok=false。
func UIDFrom(ctx context.Context) (int64, bool) {
	uid, ok := ctx.Value(ctxKey{}).(int64)
	return uid, ok
}

var _ = http.StatusOK // 保留 net/http 语义记忆：Gin 的 c.Writer 仍是 http.ResponseWriter

// OptionalAuth 可选认证：有合法 token 就注入 uid，没有/非法也放行（uid 缺省 0）。
// 用于公开接口（Feed/详情）需要"当前用户视角"的场景——不能像 GinAuth 一样 401 拦截。
func OptionalAuth(jm *jwtutil.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if strings.HasPrefix(header, prefix) {
			if uid, err := jm.Parse(strings.TrimPrefix(header, prefix), jwtutil.TypAccess); err == nil {
				ctx := context.WithValue(c.Request.Context(), ctxKey{}, uid)
				c.Request = c.Request.WithContext(ctx)
			}
		}
		c.Next()
	}
}
