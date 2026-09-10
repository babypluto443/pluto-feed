package handler

import (
	"net/http"

	"pluto_feed/internal/apierror"
)

// HealthOK GET /healthz 的响应体（router 里内联注册，这里只出响应格式）。
func HealthOK(w http.ResponseWriter) {
	apierror.OK(w, map[string]string{"status": "up"})
}
