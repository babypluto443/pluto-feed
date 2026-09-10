// Package middleware 提供 HTTP 中间件链。
package middleware

import (
	"net/http"
)

// Chain 按顺序包裹 handler：Chain(h, A, B) = A(B(h))，即声明的第一个中间件最外层。
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
