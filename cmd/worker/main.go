// Worker 进程（T4.1 双进程拆分）：与 API server 独立部署、独立扩缩容。
//
// 职责（受理与执行分离——API 只受理并落 outbox，Worker 负责一切执行）：
//   relay:    outbox → MQ（T4.4 Outbox 投递）
//   consumer: 计数事件消费（T4.3 计数异步化）
//   dlq:      死信兜底（T4.5）
//
// 启动：go run ./cmd/worker（与 cmd/server 并行运行）
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"pluto_feed/internal/cache"
	"pluto_feed/internal/config"
	"pluto_feed/internal/mq"
	"pluto_feed/internal/repository"
	"pluto_feed/internal/worker"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("[worker] load config: %v", err)
	}

	// 1. 连 MySQL（计数落库用）——fail-fast
	db, err := gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("[worker] connect mysql: %v", err)
	}

	// 2. 连 MQ —— fail-fast（Worker 没有 MQ 就没有存在的意义）
	conn, err := mq.Connect(cfg.RabbitURL)
	if err != nil {
		log.Fatalf("[worker] connect rabbitmq: %v", err)
	}
	defer conn.Close()
	publisher, err := mq.NewPublisher(conn) // 内含拓扑声明 + confirm 模式
	if err != nil {
		log.Fatalf("[worker] setup topology: %v", err)
	}

	// 3. 缓存连接（计数变更后失效帖子缓存）；Redis 不可用不阻塞启动
	redisCache := cache.NewRedis(cfg.RedisAddr)

	// 4. 组装三角色，共享一个优雅退出信号
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	outbox := repository.NewOutboxRepo(db)
	counter := worker.NewCounter(db, redisCache)

	go func() {
		if err := worker.RunRelay(ctx, db, publisher, outbox, 500*time.Millisecond); err != nil && ctx.Err() == nil {
			log.Fatalf("[worker] relay: %v", err)
		}
	}()
	go func() {
		if err := worker.RunConsumer(ctx, counter, publisher.Channel()); err != nil && ctx.Err() == nil {
			log.Fatalf("[worker] consumer: %v", err)
		}
	}()
	go func() {
		if err := worker.RunDLQ(ctx, publisher.Channel()); err != nil && ctx.Err() == nil {
			log.Fatalf("[worker] dlq: %v", err)
		}
	}()

	log.Printf("[worker] started (relay=500ms, rabbit=%s)", cfg.RabbitURL)
	<-ctx.Done()
	log.Printf("[worker] shutting down")
}
