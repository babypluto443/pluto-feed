package middleware

import (
	"log"
	"net/http"

	"pluto_feed/internal/apierror"
)

// Recover 捕获 handler panic：进程不死、返回 500。必须放在链最外层。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[recover] panic on %s %s: %v", r.Method, r.URL.Path, rec)
				apierror.Fail(w, apierror.New(http.StatusInternalServerError, 50001, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
