package middleware

import (
	"log"
	"net/http"
	"time"
)

// statusWriter 包装 ResponseWriter 以截获状态码。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// Logger 结构化访问日志：一行一条 {method path status 耗时}。
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		log.Printf("[access] method=%s path=%s status=%d duration=%s",
			r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}
