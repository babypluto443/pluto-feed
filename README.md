# pluto_feed

> 一个类小红书风格的图文社区，Go 后端 + Vue3 前端，六容器一键部署。

![Go](https://img.shields.io/badge/Go-1.26-00ADD8) ![Gin](https://img.shields.io/badge/Gin-v1-00ADD8) ![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1) ![Redis](https://img.shields.io/badge/Redis-7-DC382D) ![RabbitMQ](https://img.shields.io/badge/RabbitMQ-3-FF6600) ![Vue](https://img.shields.io/badge/Vue-3-42B883) ![Docker](https://img.shields.io/badge/Docker-Compose-2496ED)

## 项目简介

pluto_feed 实现了一个图文社区的核心业务：发布帖子、双列 Feed 流、点赞评论、关注社交、中文全文搜索。后端采用 **API / Worker 双进程架构**——API 进程只负责受理请求并把业务数据与事件在同一事务内落库，计数更新、缓存失效等衍生操作由 Worker 进程异步完成，两者通过 Outbox 模式 + RabbitMQ 衔接，保证业务数据与消息的最终一致。

前端为 Material 3 视觉风格的 SPA，覆盖完整用户旅程：注册登录、浏览发布、互动、个人主页、搜索。

## 架构

```
                        ┌─────────────────────────────────────────┐
 宿主机 :8081           │           nginx 容器                     │
 ────────────────▶      │  / → Vue3 dist (SPA)                     │
                        │  ^~ /api /uploads /healthz → server:8080 │
                        └──────────────────────┬──────────────────┘
                                               │
┌──────────────┐   同事务双写   ┌──────────────▼─────────────┐
│ server (Gin) │──────────────▶│  MySQL 8.0                 │
│  受理 + 落库  │  业务行+事件行 │  (含 ngram FULLTEXT 索引)  │
└──────┬───────┘               └────────▲───────────────────┘
       │ relay 轮询（confirm 模式）      │ 计数 UPDATE + 缓存失效
       ▼                                │
┌──────────────┐    消费 / 去重  ┌───────┴────────────┐
│ RabbitMQ     │───────────────▶│ worker             │
│ pluto.events │                │ relay / consumer / │
│ + DLX/DLQ    │                │ dlq 三角色          │
└──────────────┘                └────────────────────┘
                                         │
                                  ┌──────▼──────┐
                                  │ Redis 7     │ 缓存 / 热榜 / 限流 / 白名单
                                  └─────────────┘
```

完整版（含死信路径）见 `diagrams/m4-outbox-architecture.png`。

### 可靠性设计

- **Outbox 模式**：业务行与事件行同事务提交，用本地事务的原子性保证"业务成功 ⇔ 事件必存在"
- **恰好一次效果**：confirm 模式可靠投递（至少一次）+ `processed_events` 去重表幂等消费 = 不丢也不重
- **死信兜底**：消费失败 NACK 不重入队，经 DLX 路由到死信队列，毒消息不会拖垮消费者
- **缓存一致性**：Cache Aside 写时失效；未命中路径经布隆过滤器拦截（不存在的 id 不触达数据库）
- **优雅降级**：Redis / 布隆 / 白名单任一组件缺席时自动退化为直连模式，不宕机

### 安全设计

- **限流**：双层策略——每 IP 固定窗口（全站）+ 每用户滑动窗口（写操作，ZSET 实现）；Redis 故障时自动放行
- **认证**：JWT 双 token（access + refresh），refresh token 白名单轮换——每次刷新作废旧 token，泄露可止损；`jti` 唯一 ID 防同秒重放
- **密码**：bcrypt 哈希；登录失败统一文案，不泄露账号是否存在
- **输入**：计数字段白名单校验（防 SQL 注入）、搜索词截断、上传文件魔数与大小校验
- **容器**：非 root 用户运行，静态编译减小攻击面

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26 · Gin · GORM · singleflight |
| 数据 | MySQL 8.0（ngram 全文索引）· Redis 7 · RabbitMQ 3 |
| 前端 | Vue 3 · TypeScript · Vite · Pinia · Vue Router · Axios · Naive UI |
| 部署 | Docker 多阶段构建 · nginx 反代 · Docker Compose |

## 快速开始

### 容器化一键启动（推荐）

前置：Docker + Docker Compose

```bash
git clone https://github.com/babypluto443/pluto-feed-go.git
cd pluto-feed-go
docker compose up -d --build
```

完成后访问 **http://localhost:8081**。MySQL / Redis / RabbitMQ 首次启动自动建表，无需手工初始化。

```bash
docker compose ps                # 六容器状态（server/nginx 带 healthcheck）
docker compose logs -f worker    # 跟踪异步消费日志
docker compose down -v           # 彻底销毁（含数据卷）
```

> docker-compose.yml 中的数据库密码为本地开发默认值，生产部署请改为环境变量注入。

### 本地开发模式

```bash
# 1. 基础设施（宿主机映射 3308/6380/5673）
docker compose up -d mysql redis rabbitmq

# 2. 后端双进程（读取 config.yaml，模板见 config.example.yaml）
go run ./cmd/server    # API :8080
go run ./cmd/worker    # 异步消费

# 3. 前端（vite dev 代理 /api 到 :8080）
cd web && npm install && npm run dev   # http://localhost:5173
```

## 功能

| 模块 | 能力 |
|---|---|
| 认证 | 注册 / 登录 / 双 token / 刷新轮换 / 登出全端吊销 |
| 帖子 | 发布（1~9 图）/ 详情 / 软删除 / 标签 |
| Feed | 推荐流（游标分页，深翻页 O(1)）/ 关注流 / 热榜（分钟桶聚合）/ 无限滚动 |
| 互动 | 点赞（幂等 + 异步计数）/ 评论（游标分页）/ 删除 |
| 社交 | 关注 / 取关 / 关注列表 / 粉丝列表 / 个人主页聚合 |
| 搜索 | 中文全文搜索（ngram 分词） |
| 前端 | 登录态持久化 / 点赞乐观更新 / 骨架与空态 / Material 3 视觉 |

## API

统一响应格式：`{"code":0,"msg":"ok","data":{...}}`，错误码经注册表模式从唯一出口输出。

| 方法 | 路径 | 说明 | 认证 |
|---|---|---|---|
| POST | `/api/v1/auth/register` | 注册 | - |
| POST | `/api/v1/auth/login` | 登录 | - |
| POST | `/api/v1/auth/refresh` | 刷新（轮换） | - |
| POST | `/api/v1/auth/logout` | 登出吊销 | ✅ |
| GET | `/api/v1/me` | 当前用户 | ✅ |
| POST | `/api/v1/upload` | 图片上传 | ✅ |
| POST | `/api/v1/posts` | 发帖 | ✅ |
| GET | `/api/v1/posts/:id` | 帖子详情 | 可选 |
| DELETE | `/api/v1/posts/:id` | 删帖（属主） | ✅ |
| POST/DELETE | `/api/v1/posts/:id/like` | 点赞/取消 | ✅ |
| GET/POST | `/api/v1/posts/:id/comments` | 评论列表/发表 | 列表公开 |
| DELETE | `/api/v1/comments/:id` | 删评论（属主） | ✅ |
| GET | `/api/v1/feed` | 推荐流（游标） | 可选 |
| GET | `/api/v1/feed/following` | 关注流 | ✅ |
| GET | `/api/v1/feed/hot` | 热榜 | 可选 |
| POST/DELETE | `/api/v1/users/:id/follow` | 关注/取关 | ✅ |
| GET | `/api/v1/users/:id/following` / `followers` | 关注/粉丝列表 | 公开 |
| GET | `/api/v1/users/:id/profile` | 个人主页 | 可选 |
| GET | `/api/v1/search?q=` | 全文搜索 | 可选 |

## 性能

ab 压测（2000 请求 / 50 并发，帖子详情接口）：

| 场景 | QPS | P99 |
|---|---|---|
| 直连 MySQL | 3,370 | 21ms |
| Redis 缓存命中 | **13,704（4.1×）** | 21ms |

压测脚本：`test/m3_bench.sh`

## 测试

```bash
go vet ./... && go test ./...     # 单元测试（限流 / 布隆 / 轮换 / 服务层）

# e2e 验收脚本（默认打 :8080；容器栈用 BASE 指向 :8081）
BASE=http://localhost:8081 bash test/m1_e2e.sh   # 认证 / 发帖 / 游标分页 / 幂等 / 删帖（18 断言）
BASE=http://localhost:8081 bash test/m2_e2e.sh   # 关注 / 双流 / 主页聚合（19 断言）
BASE=http://localhost:8081 bash test/m4_e2e.sh   # 异步计数轮询 / 幂等 / outbox 清零（14 断言）
bash test/m3_bench.sh                            # 缓存命中 vs 直连压测对照
```

测试纪律：service 层依赖注入假实现（真实仓储改语义，假实现必须同步）；时间依赖逻辑时钟可注入；miniredis 驱动真实 Redis 语义。

## 目录结构

```
pluto_feed/
├── cmd/
│   ├── server/          # API 进程（Gin）
│   └── worker/          # 异步进程（relay / consumer / dlq）
├── internal/
│   ├── apierror/        # 统一错误出口（注册表模式）
│   ├── bloom/           # 布隆过滤器（防穿透）
│   ├── cache/           # Redis 封装
│   ├── config/          # 配置加载（yaml 可选，环境变量覆盖）
│   ├── handler/         # HTTP 层：绑参数、调 service、写响应
│   ├── jwtutil/         # JWT 双 token（含 jti）
│   ├── middleware/      # 认证 / 可选认证 / 双层限流
│   ├── model/           # 数据模型 + 事件常量
│   ├── mq/              # RabbitMQ 拓扑 + confirm 发布器
│   ├── repository/      # 数据访问（GORM）
│   ├── router/          # 路由组装
│   ├── service/         # 业务逻辑（框架无关）
│   └── worker/          # 异步三角色
├── migrations/          # SQL 迁移（容器 initdb 自动执行）
├── web/                 # Vue3 前端（+ nginx.conf 生产配置）
├── test/                # e2e 验收脚本 + 压测
├── diagrams/            # 架构图
├── Dockerfile           # 后端多阶段构建
└── docker-compose.yml   # 六容器编排
```

## License

MIT
