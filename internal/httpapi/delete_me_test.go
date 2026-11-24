package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

// fakeAdmin은 Supabase Auth Admin API(DELETE /auth/v1/admin/users/{id})를 흉내낸다.
type fakeAdmin struct {
	mu      sync.Mutex
	status  int
	users   map[string]bool // 남아 있는 사용자
	deletes []string
	srv     *httptest.Server
}

const adminKey = "test-service-role-key"

func newFakeAdmin(t *testing.T) *fakeAdmin {
	t.Helper()
	f := &fakeAdmin{status: http.StatusOK, users: map[string]bool{storetest.UserA: true, storetest.UserB: true}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id, ok := strings.CutPrefix(r.URL.Path, "/auth/v1/admin/users/")
		if r.Method != http.MethodDelete || !ok || r.Header.Get("apikey") != adminKey || r.Header.Get("Authorization") != "Bearer "+adminKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.deletes = append(f.deletes, id)
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			return
		}
		if !f.users[id] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.users, id)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAdmin) setStatus(code int) { f.mu.Lock(); defer f.mu.Unlock(); f.status = code }

func TestDeleteMe(t *testing.T) {
	admin := newFakeAdmin(t)
	st := memory.New(storetest.Fixtures()...)
	var buf syncBuf
	srv := httptest.NewServer(httpapi.New(st, slog.New(slog.NewTextHandler(&buf, nil)),
		httpapi.WithClock(func() time.Time { return storetest.Now }),
		httpapi.WithAuth(testVerifier), httpapi.WithSaved(st),
		httpapi.WithUserDeleter(auth.NewSupabaseAdmin(admin.srv.URL, adminKey, nil))).Handler())
	defer srv.Close()

	for _, tok := range []string{tokA, tokB} {
		for _, id := range []string{"test:mid-popup", "test:ended"} {
			if res := call(t, srv, http.MethodPut, "/v1/me/saved/"+id, tok, nil, nil); res.StatusCode != http.StatusNoContent {
				t.Fatalf("put: %d", res.StatusCode)
			}
		}
	}
	count := func(tok string) int {
		var p savedPage
		call(t, srv, http.MethodGet, "/v1/me/saved", tok, nil, &p)
		return len(p.Items)
	}

	// 토큰 없이는 401, 관리 API는 부르지 않는다.
	if res := call(t, srv, http.MethodDelete, "/v1/me", "", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.StatusCode)
	}

	// 관리 API 실패 → 502, 저장 행은 남는다.
	admin.setStatus(http.StatusInternalServerError)
	var e apiError
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokA, nil, &e); res.StatusCode != http.StatusBadGateway || e.Code != "upstream_error" {
		t.Fatalf("admin failure: %d %q", res.StatusCode, e.Code)
	}
	if n := count(tokA); n != 2 {
		t.Fatalf("saved rows must be kept when admin delete fails, got %d", n)
	}

	// 성공 → 204, 사용자 A의 저장만 지워진다.
	admin.setStatus(http.StatusOK)
	res := call(t, srv, http.MethodDelete, "/v1/me", tokA, nil, nil)
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("delete: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if count(tokA) != 0 || count(tokB) != 2 {
		t.Fatalf("after delete: A=%d B=%d", count(tokA), count(tokB))
	}
	if admin.users[storetest.UserA] || !admin.users[storetest.UserB] {
		t.Fatalf("admin users = %v", admin.users)
	}

	// 재시도(토큰이 아직 유효): 관리 API 404는 성공으로 보고 204.
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("retry: %d", res.StatusCode)
	}
	if want := []string{storetest.UserA, storetest.UserA, storetest.UserA}; !slices.Equal(admin.deletes, want) {
		t.Fatalf("admin calls = %v, want %v", admin.deletes, want)
	}

	srv.Close()
	if out := buf.String(); strings.Contains(out, adminKey) || strings.Contains(out, storetest.UserA) || strings.Contains(out, admin.srv.URL) {
		t.Fatalf("log leaks key, user id or admin URL:\n%s", out)
	}
}

func TestDeleteMeNotConfigured(t *testing.T) {
	st := memory.New(storetest.Fixtures()...)
	srv := httptest.NewServer(httpapi.New(st, slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpapi.WithAuth(testVerifier), httpapi.WithSaved(st)).Handler())
	defer srv.Close()
	var e apiError
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokA, nil, &e); res.StatusCode != http.StatusServiceUnavailable || e.Code != "auth_not_configured" {
		t.Fatalf("got %d %q", res.StatusCode, e.Code)
	}
	if res := call(t, srv, http.MethodDelete, "/v1/me", "", nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.StatusCode)
	}
}
