package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"pluto_feed/internal/apierror"
)

// 上传限制（与实施计划任务 13 一致）
const (
	MaxUploadSize = 5 << 20 // 5MB
)

// 允许的图片类型：Content-Type（http.DetectContentType 嗅探结果）→ 文件扩展名
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
}

// UploadService 图片上传：校验 + 落盘，返回可访问的 URL 路径。
type UploadService struct {
	rootDir string // 如 data/uploads
}

func NewUploadService(rootDir string) *UploadService {
	return &UploadService{rootDir: rootDir}
}

// Save 处理一个上传的文件。
// 安全三道关：
//  1. 大小：header.Size 超限直接拒（配合 handler 里的 MaxBytesReader 双保险）
//  2. 类型：不信文件名后缀（可伪造），用 http.DetectContentType 读文件头 512 字节嗅探真实类型
//  3. 文件名：不用用户上传的原始文件名（可带 ../ 路径穿越），服务端自己生成随机名
func (s *UploadService) Save(fh *multipart.FileHeader) (string, error) {
	if fh.Size > MaxUploadSize {
		return "", apierror.New(413, 41300, "文件超过 5MB 限制")
	}

	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	// 嗅探真实类型：扩展名叫 .jpg 的文件内容可能是任何东西
	buf := make([]byte, 512)
	n, err := src.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	detected := http.DetectContentType(buf[:n])
	ext, ok := allowedImageTypes[detected]
	if !ok {
		return "", apierror.New(415, 41500, "仅支持 jpg/png 图片")
	}

	// 服务端生成文件名：随机 16 字节 hex，杜绝路径穿越与重名覆盖
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	dir := filepath.Join(s.rootDir, time.Now().Format("200601"))
	name := hex.EncodeToString(random) + ext

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	defer dst.Close()

	// 把已读的头部 + 剩余内容全部写入（头部那 512 字节也是文件的一部分）
	if _, err := dst.Write(buf[:n]); err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	// 返回 URL 路径（main.go 把 /uploads/ 挂到 rootDir 静态目录）
	return fmt.Sprintf("/uploads/%s/%s", time.Now().Format("200601"), name), nil
}
