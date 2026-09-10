package main

import (
	"context"
	"encoding/binary"
	"log"
	"net/http"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"pluto_feed/internal/bloom"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/config"
	"pluto_feed/internal/model"
	"pluto_feed/internal/jwtutil"
	"pluto_feed/internal/repository"
	"pluto_feed/internal/router"
	"pluto_feed/internal/service"
)

const uploadDir = "data/uploads"

func main() {
	// 1. 加载配置（env > yaml > 默认值），必填项缺失直接退出
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("[startup] load config: %v", err)
	}

	// 2. 连接数据库：启动期 fail-fast，连不上就不服务
	db, err := gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("[startup] connect mysql: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("[startup] get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	log.Printf("[startup] mysql connected")

	// 3. 组装依赖：jwt 管理器 → repo → service → router（依赖注入）
	jm := jwtutil.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	userRepo := repository.NewUserRepo(db)
	postRepo := repository.NewPostRepo(db)
	socialRepo := repository.NewSocialRepo(db)
	outboxRepo := repository.NewOutboxRepo(db)
	interactionRepo := repository.NewInteractionRepo(db, outboxRepo)

	// 缓存层（T3.1）：Redis 不可用时各 service 内部自动降级直连 DB
	redisCache := cache.NewRedis(cfg.RedisAddr)

	// 布隆过滤器（O3）：启动加载全量 post id（表空/小表成本可忽略），防恶意 id 穿透
	postBloom, err := bloom.New(1_000_000, 0.01)
	if err != nil {
		log.Fatalf("[startup] bloom: %v", err)
	}
	var postIDs []int64
	if err := db.WithContext(context.Background()).Model(&model.Post{}).Pluck("id", &postIDs).Error; err != nil {
		log.Fatalf("[startup] load post ids: %v", err)
	}
	for _, id := range postIDs {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(id))
		postBloom.Add(b)
	}
	log.Printf("[startup] bloom loaded %d post ids", len(postIDs))

	authSvc := service.NewAuthService(userRepo, jm, redisCache)
	postSvc := service.NewPostService(postRepo, userRepo, redisCache, interactionRepo, postBloom)
	feedSvc := service.NewFeedService(postRepo, userRepo, socialRepo, redisCache, interactionRepo)
	interactionSvc := service.NewInteractionService(postRepo, userRepo, interactionRepo, redisCache)
	socialSvc := service.NewSocialService(userRepo, socialRepo, redisCache)
	profileSvc := service.NewProfileService(postRepo, userRepo, socialRepo, interactionRepo)
	uploadSvc := service.NewUploadService(uploadDir)

	// 上传目录不存在就创建（启动期保证，运行期只管写文件）
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Fatalf("[startup] mkdir uploads: %v", err)
	}

	srv := router.New(router.Deps{
		Auth:        authSvc,
		JWT:         jm,
		Post:        postSvc,
		Feed:        feedSvc,
		Upload:      uploadSvc,
		Interaction: interactionSvc,
		Social:      socialSvc,
		Profile:        profileSvc,
		RDB:            redisCache.Client(),
		RateIPPerMin:   cfg.RateIPPerMin,
		RateUserPerMin: cfg.RateUserPerMin,
		UploadDir:      uploadDir,
	})

	// 4. 启动 HTTP 服务
	log.Printf("[startup] listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, srv); err != nil {
		log.Fatalf("[startup] server: %v", err)
	}
}
