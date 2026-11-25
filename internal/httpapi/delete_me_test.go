package httpapi_test

import (
	"context"
	"errors"
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

// providerVerifier: testVerifier에 제공자를 붙인다(Apple 사용자 = UserA).
type providerVerifier map[string]auth.User

func (p providerVerifier) Verify(_ context.Context, tok string) (auth.User, error) {
	if u, ok := p[tok]; ok {
		return u, nil
	}
	return auth.User{}, auth.ErrInvalidToken
}

// fakeRevoker는 auth.AppleRevoker. "bad"는 ErrAppleInvalidCode, err가 있으면 그 에러.
type fakeRevoker struct {
	mu    sync.Mutex
	err   error
	codes []string
}

func (f *fakeRevoker) RevokeAuthorizationCode(_ context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = append(f.codes, code)
	if code == "bad" {
		return auth.ErrAppleInvalidCode
	}
	return f.err
}

func (f *fakeRevoker) setErr(err error) { f.mu.Lock(); defer f.mu.Unlock(); f.err = err }

func TestDeleteMeApple(t *testing.T) {
	admin := newFakeAdmin(t)
	st := memory.New(storetest.Fixtures()...)
	rev := &fakeRevoker{}
	v := providerVerifier{
		tokA: {ID: storetest.UserA, Providers: []string{"apple"}},
		tokB: {ID: storetest.UserB, Providers: []string{"kakao"}},
	}
	var buf syncBuf
	newSrv := func(opts ...httpapi.Option) *httptest.Server {
		base := []httpapi.Option{httpapi.WithAuth(v), httpapi.WithSaved(st),
			httpapi.WithUserDeleter(auth.NewSupabaseAdmin(admin.srv.URL, adminKey, nil))}
		s := httptest.NewServer(httpapi.New(st, slog.New(slog.NewTextHandler(&buf, nil)), append(base, opts...)...).Handler())
		t.Cleanup(s.Close)
		return s
	}
	srv := newSrv(httpapi.WithAppleRevoker(rev))
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:mid-popup", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d", res.StatusCode)
	}
	type body = map[string]any
	expect := func(name string, s *httptest.Server, tok string, b any, status int, code string) {
		t.Helper()
		var e apiError
		res := call(t, s, http.MethodDelete, "/v1/me", tok, b, &e)
		if res.StatusCode != status || e.Code != code {
			t.Fatalf("%s: got %d %q, want %d %q", name, res.StatusCode, e.Code, status, code)
		}
	}

	// 서버에 Apple 키가 없으면 Apple 사용자는 503, 지우지 않는다.
	expect("not configured", newSrv(), tokA, body{"apple_authorization_code": "c"}, http.StatusServiceUnavailable, "auth_not_configured")
	// 코드 없음·만료 코드 → 400 apple_reauth_required.
	expect("no body", srv, tokA, nil, http.StatusBadRequest, "apple_reauth_required")
	expect("empty code", srv, tokA, body{"apple_authorization_code": ""}, http.StatusBadRequest, "apple_reauth_required")
	expect("invalid code", srv, tokA, body{"apple_authorization_code": "bad"}, http.StatusBadRequest, "apple_reauth_required")
	expect("bad json", srv, tokA, "not an object", http.StatusBadRequest, "invalid_parameter")
	expect("code too long", srv, tokA, body{"apple_authorization_code": strings.Repeat("x", 3000)}, http.StatusBadRequest, "invalid_parameter")
	// Apple 서버 오류 → 502.
	rev.setErr(errors.New("auth: apple revoke: status 503"))
	expect("apple down", srv, tokA, body{"apple_authorization_code": "code-1"}, http.StatusBadGateway, "upstream_error")
	if len(admin.deletes) != 0 || !admin.users[storetest.UserA] {
		t.Fatalf("nothing must be deleted before Apple revoke succeeds: %v", admin.deletes)
	}
	var p savedPage
	if call(t, srv, http.MethodGet, "/v1/me/saved", tokA, nil, &p); len(p.Items) != 1 {
		t.Fatalf("saved rows must be kept: %d", len(p.Items))
	}

	// 성공: 철회 → Supabase 삭제 → 204.
	rev.setErr(nil)
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokA, body{"apple_authorization_code": "code-2"}, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("apple delete: %d", res.StatusCode)
	}
	if !slices.Equal(rev.codes, []string{"bad", "code-1", "code-2"}) || !slices.Equal(admin.deletes, []string{storetest.UserA}) {
		t.Fatalf("revoke codes %v, admin deletes %v", rev.codes, admin.deletes)
	}

	// 카카오 사용자: 코드 없이 탈퇴, 철회하지 않는다(코드를 보내도 무시).
	if res := call(t, srv, http.MethodDelete, "/v1/me", tokB, body{"apple_authorization_code": "ignored"}, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("kakao delete: %d", res.StatusCode)
	}
	if len(rev.codes) != 3 {
		t.Fatalf("kakao user must not trigger revoke: %v", rev.codes)
	}
	if out := buf.String(); strings.Contains(out, "code-1") || strings.Contains(out, storetest.UserA) {
		t.Fatalf("log leaks code or user id:\n%s", out)
	}
}
