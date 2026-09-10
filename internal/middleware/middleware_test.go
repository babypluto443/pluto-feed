package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecover_PanicBecomes500AndProcessSurvives(t *testing.T) {
	panicky := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	srv := Chain(panicky, Recover)

	req := httptest.NewRequest("GET", "/boom", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req) // 关键：不 panic、测试进程不死

	if w.Code != 500 {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestRecover_NormalRequestUnaffected(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("fine"))
	})
	srv := Chain(ok, Recover)

	req := httptest.NewRequest("GET", "/ok", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 || w.Body.String() != "fine" {
		t.Errorf("got %d %q, want 200 fine", w.Code, w.Body.String())
	}
}

func TestLogger_RecordsStatus(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
	}), Logger)

	req := httptest.NewRequest("GET", "/created", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("status = %d, want 201 (logger must not swallow status)", w.Code)
	}
}

func TestChain_Order(t *testing.T) {
	// Chain(h, A, B) 应为 A(B(h))：外层先执行
	var order []string
	A := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "A")
			next.ServeHTTP(w, r)
		})
	}
	B := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "B")
			next.ServeHTTP(w, r)
		})
	}
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "H")
	}), A, B)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	want := []string{"A", "B", "H"}
	for i, v := range want {
		if order[i] != v {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
