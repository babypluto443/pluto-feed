// Package router 组装所有路由与中间件（M5：net/http ServeMux → Gin Engine）。
//
// 迁移映射（T5.1，完整对照表见 09-M5-开发记录.md）：
//   http.NewServeMux + "GET /posts/{id}" → gin.New() + "/posts/:id"
//   middleware.Chain(Recover, Logger)    → gin.Recovery() + gin.Logger()
//   middleware.Auth(jwt)(http.HandlerFunc) → GinAuth(jm) 中间件挂载
//   http.FileServer(StripPrefix)         → engine.Static
//   手写 bindJSON                        → c.ShouldBindJSON
package router

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"pluto_feed/internal/handler"
	"pluto_feed/internal/jwtutil"
	"pluto_feed/internal/middleware"
	"pluto_feed/internal/service"
)

// Deps router 依赖的业务对象，由 main 组装后注入。
type Deps struct {
	Auth        *service.AuthService
	JWT         *jwtutil.Manager
	Post        *service.PostService
	Feed        *service.FeedService
	Upload      *service.UploadService
	Interaction *service.InteractionService
	Social      *service.SocialService
	Profile        *service.ProfileService
	RDB            *redis.Client // 限流用（可为 nil = 关闭限流）
	RateIPPerMin   int
	RateUserPerMin int
	UploadDir      string // 静态图片目录（如 data/uploads）
}

// New 组装 Gin 引擎：中间件 + 各接口。返回值仍实现 http.Handler（main 无缝衔接）。
func New(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery()) // 对标旧链：Logger → Recover（Recover 必须最外层兜 panic）
	if d.RDB != nil && d.RateIPPerMin > 0 {
		r.Use(middleware.RateLimitIP(d.RDB, d.RateIPPerMin)) // O1 第一层：每 IP 固定窗口
	}

	// 健康检查
	r.GET("/healthz", func(c *gin.Context) {
		handler.HealthOK(c.Writer)
	})

	authed := r.Group("/", middleware.GinAuth(d.JWT)) // 受保护路由统一挂 Auth
	uidFrom := func(c *gin.Context) (int64, bool) { return middleware.UIDFrom(c.Request.Context()) }
	// O1 第二层：敏感写操作按用户滑动窗口（只对写方法生效）
	authedWrite := r.Group("/", middleware.GinAuth(d.JWT),
		middleware.RateLimitUser(d.RDB, uidFrom, d.RateUserPerMin, time.Minute))

	// —— 认证（公开）——
	ah := handler.NewAuthHandler(d.Auth)
	r.POST("/api/v1/auth/register", ah.Register)
	r.POST("/api/v1/auth/login", ah.Login)
	r.POST("/api/v1/auth/refresh", ah.Refresh)
	authed.GET("/api/v1/me", ah.Me)        // 当前登录用户（前端登录态用）
	authed.POST("/api/v1/auth/logout", ah.Logout) // 登出：吊销 refresh token

	// —— 上传（受保护）——
	uh := handler.NewUploadHandler(d.Upload)
	authed.POST("/api/v1/upload", uh.Upload)

	// —— 帖子 ——
	ph := handler.NewPostHandler(d.Post)
	authedWrite.POST("/api/v1/posts", ph.Create)         // 发帖：登录
	authedWrite.DELETE("/api/v1/posts/:id", ph.Delete)   // 删帖：登录+属主
	r.GET("/api/v1/posts/:id", middleware.OptionalAuth(d.JWT), ph.Get) // 详情：公开（可选登录视角）

	// —— 互动 ——
	ih := handler.NewInteractionHandler(d.Interaction)
	authedWrite.POST("/api/v1/posts/:id/like", ih.Like)          // 点赞：登录
	authedWrite.DELETE("/api/v1/posts/:id/like", ih.Unlike)      // 取消：登录
	authedWrite.POST("/api/v1/posts/:id/comments", ih.Comment)   // 评论：登录
	authedWrite.DELETE("/api/v1/comments/:id", ih.DeleteComment) // 删评论：登录+属主
	r.GET("/api/v1/posts/:id/comments", ih.ListComments)    // 评论列表：公开

	// —— Feed ——
	fh := handler.NewFeedHandler(d.Feed)
	r.GET("/api/v1/feed", middleware.OptionalAuth(d.JWT), fh.List)         // 推荐流：公开
	r.GET("/api/v1/feed/hot", middleware.OptionalAuth(d.JWT), fh.ListHot)  // 热榜：公开

	// —— 搜索（O4）——
	sh2 := handler.NewSearchHandler(d.Post)
	r.GET("/api/v1/search", middleware.OptionalAuth(d.JWT), sh2.Search)
	authed.GET("/api/v1/feed/following", fh.ListFollowing) // 关注流：必须登录

	// —— 社交（M2）——
	sh := handler.NewSocialHandler(d.Social)
	authed.POST("/api/v1/users/:id/follow", sh.Follow)     // 关注：登录
	authed.DELETE("/api/v1/users/:id/follow", sh.Unfollow) // 取关：登录
	r.GET("/api/v1/users/:id/following", sh.ListFollowing) // 关注列表：公开
	r.GET("/api/v1/users/:id/followers", sh.ListFollowers) // 粉丝列表：公开

	// —— 个人主页（公开）——
	pfh := handler.NewProfileHandler(d.Profile)
	r.GET("/api/v1/users/:id/profile", pfh.Get)

	// —— 静态图片（对标旧 http.StripPrefix + FileServer）——
	r.Static("/uploads", d.UploadDir)

	return r
}
