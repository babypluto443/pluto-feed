// Package apierror 提供统一的业务错误定义与 HTTP 响应出口。
// 所有 handler 出口只允许两个函数：OK / Fail，保证响应结构一致。
package apierror

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// BizError 业务错误：携带 HTTP 状态码与业务码。
type BizError struct {
	HTTPStatus int    // 写给 HTTP 层
	Code       int    // 写给前端/调用方的业务码
	Msg        string
}

func (e *BizError) Error() string { return e.Msg }

// 哨兵错误：全项目复用，handler/service 层直接引用。
var (
	ErrBadRequest   = &BizError{HTTPStatus: http.StatusBadRequest, Code: 40000, Msg: "bad request"}
	ErrUnauthorized = &BizError{HTTPStatus: http.StatusUnauthorized, Code: 40100, Msg: "unauthorized"}
	ErrForbidden    = &BizError{HTTPStatus: http.StatusForbidden, Code: 40300, Msg: "forbidden"}
	ErrNotFound     = &BizError{HTTPStatus: http.StatusNotFound, Code: 40400, Msg: "not found"}
)

// New 创建一个自定义业务错误（如"账号或密码错误"）。
func New(status, code int, msg string) *BizError {
	return &BizError{HTTPStatus: status, Code: code, Msg: msg}
}

// Response 统一响应结构。
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// OK 成功响应：{"code":0,"msg":"ok","data":...}
func OK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response{Code: 0, Msg: "ok", Data: data})
}

// Fail 唯一的错误出口：
//   - *BizError（或包着它的 error 链）→ 按其 HTTPStatus/Code 输出
//   - 其他未知 error → 记日志，输出 500（不向客户端泄露内部细节）
func Fail(w http.ResponseWriter, err error) {
	var be *BizError
	if errors.As(err, &be) {
		writeJSON(w, be.HTTPStatus, Response{Code: be.Code, Msg: be.Msg})
		return
	}
	log.Printf("[apierror] unexpected error: %v", err)
	writeJSON(w, http.StatusInternalServerError, Response{Code: 50000, Msg: "internal error"})
}
