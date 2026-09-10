package apierror

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestFail_BizError(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, ErrUnauthorized)

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if resp.Code != 40100 || resp.Msg != "unauthorized" {
		t.Errorf("resp = %+v, want code 40100 msg unauthorized", resp)
	}
}

func TestFail_WrappedBizError(t *testing.T) {
	// 用 fmt.Wrap 包一层，errors.As 仍应识别出 BizError
	w := httptest.NewRecorder()
	Fail(w, fmt.Errorf("query user: %w", ErrForbidden))

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestFail_UnknownErrorBecomes500(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, errors.New("db connection refused"))

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
	// 内部细节不泄露
	if got := w.Body.String(); contains(got, "db connection refused") {
		t.Errorf("body leaks internal error: %s", got)
	}
}

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, map[string]string{"hello": "world"})

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if resp.Code != 0 || resp.Msg != "ok" {
		t.Errorf("resp = %+v, want code 0 msg ok", resp)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
