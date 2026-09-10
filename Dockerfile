# 多阶段构建：builder 里装着完整 Go 工具链（~1GB），运行时只拷走静态二进制（~20MB）
# 为什么能这么小：CGO_ENABLED=0 纯静态编译，二进制不依赖任何系统 so；alpine 只提供 ca-certificates 和时区
FROM golang:1.26-alpine AS builder
WORKDIR /src

# 先拷 go.mod/go.sum 单独 download：依赖不变时这层直接命中缓存，改代码不用重拉依赖
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# ---- 运行时镜像 ----
FROM alpine:3.22
# ca-certificates: 出网 HTTPS；tzdata: loc=Local 时区；wget: healthcheck 探活
RUN apk add --no-cache ca-certificates tzdata wget \
 && addgroup -S app && adduser -S -G app app  # -S 系统用户：uid 自动分配，不与基础镜像冲突

WORKDIR /app
COPY --from=builder /out/server /out/worker ./
# 上传目录：容器内路径（compose 会挂具名卷持久化），属主交给非 root 用户
RUN mkdir -p /app/data/uploads && chown -R app:app /app
USER app

EXPOSE 8080
# 默认跑 server；worker 容器在 compose 里用 command 覆盖为 /app/worker
ENTRYPOINT ["/app/server"]
