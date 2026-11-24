package httpapi_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

// fakeVerifier: 토큰 문자열 → 사용자. 모르는 토큰은 ErrInvalidToken, "down"은 ErrUnavailable.
type fakeVerifier map[string]string

func (f fakeVerifier) Verify(_ context.Context, tok string) (auth.User, error) {
	if tok == "down" {
		return auth.User{}, fmt.Errorf("%w: test", auth.ErrUnavailable)
	}
	if id, ok := f[tok]; ok {
		return auth.User{ID: id}, nil
	}
	return auth.User{}, auth.ErrInvalidToken
}

const (
	tokA = "token-a"
	tokB = "token-b"
)

var testVerifier = fakeVerifier{tokA: storetest.UserA, tokB: storetest.UserB}

type savedSpot struct {
	ID      string  `json:"id"`
	IsEnded *bool   `json:"is_ended"`
	SavedAt *string `json:"saved_at"`
}

type savedPage struct {
	Items      []savedSpot `json:"items"`
	NextCursor string      `json:"next_cursor"`
}

// meServer: 메모리 저장소 + 고정 시계. now는 테스트 안에서 바꿀 수 있다.
func meServer(t *testing.T, opts ...httpapi.Option) (*httptest.Server, *time.Time) {
	t.Helper()
	st := memory.New(storetest.Fixtures()...)
	now := storetest.Now
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	all := append([]httpapi.Option{
		httpapi.WithClock(func() time.Time { return now }),
		httpapi.WithAuth(testVerifier),
		httpapi.WithSaved(st),
	}, opts...)
	srv := httptest.NewServer(httpapi.New(st, log, all...).Handler())
	t.Cleanup(srv.Close)
	return srv, &now
}

// call sends a request with an optional bearer token and JSON body; out decodes a JSON response.
func call(t *testing.T, srv *httptest.Server, method, path, token string, body any, out any) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			j, _ := json.Marshal(b)
			rd = strings.NewReader(string(j))
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return res
}

func savedIDs(p savedPage) []string {
	out := []string{}
	for _, it := range p.Items {
		out = append(out, strings.TrimPrefix(it.ID, "test:"))
	}
	return out
}

func TestMeRequiresAuth(t *testing.T) {
	srv, _ := meServer(t)
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/v1/me/saved"},
		{http.MethodPut, "/v1/me/saved/test:mid-popup"},
		{http.MethodDelete, "/v1/me/saved/test:mid-popup"},
		{http.MethodPost, "/v1/me/saved:import"},
	} {
		for _, tc := range []struct {
			name, header string
			status       int
			code         string
		}{
			{"no header", "", http.StatusUnauthorized, "unauthorized"},
			{"not bearer", "Basic " + tokA, http.StatusUnauthorized, "unauthorized"},
			{"empty bearer", "Bearer ", http.StatusUnauthorized, "unauthorized"},
			{"invalid token", "Bearer nope", http.StatusUnauthorized, "unauthorized"},
			{"keys unavailable", "Bearer down", http.StatusServiceUnavailable, "auth_unavailable"},
		} {
			t.Run(r.method+" "+r.path+" "+tc.name, func(t *testing.T) {
				req, _ := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(`{"spot_ids":["test:mid-popup"]}`))
				if tc.header != "" {
					req.Header.Set("Authorization", tc.header)
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var e apiError
				_ = json.NewDecoder(res.Body).Decode(&e)
				if res.StatusCode != tc.status || e.Code != tc.code {
					t.Fatalf("status %d code %q, want %d %q", res.StatusCode, e.Code, tc.status, tc.code)
				}
				if tc.status == http.StatusUnauthorized && !strings.HasPrefix(res.Header.Get("WWW-Authenticate"), "Bearer") {
					t.Errorf("WWW-Authenticate = %q", res.Header.Get("WWW-Authenticate"))
				}
				if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
					t.Errorf("Cache-Control = %q", cc)
				}
			})
		}
	}
}

func TestMeNotConfigured(t *testing.T) {
	st := memory.New(storetest.Fixtures()...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.New(st, log).Handler())
	defer srv.Close()
	var e apiError
	res := call(t, srv, http.MethodGet, "/v1/me/saved", tokA, nil, &e)
	if res.StatusCode != http.StatusServiceUnavailable || e.Code != "auth_not_configured" {
		t.Fatalf("status %d code %q", res.StatusCode, e.Code)
	}
	// 공개 조회 API는 인증 설정과 무관하게 그대로 동작한다.
	var p page
	if res := get(t, srv, "/v1/spots", near(), &p); res.StatusCode != http.StatusOK {
		t.Fatalf("/v1/spots status %d", res.StatusCode)
	}
}

func TestSavedFlow(t *testing.T) {
	srv, now := meServer(t)
	put := func(tok, id string) int {
		return call(t, srv, http.MethodPut, "/v1/me/saved/"+id, tok, nil, nil).StatusCode
	}
	list := func(tok string, params url.Values) (savedPage, *http.Response) {
		var p savedPage
		path := "/v1/me/saved"
		if len(params) > 0 {
			path += "?" + params.Encode()
		}
		res := call(t, srv, http.MethodGet, path, tok, nil, &p)
		return p, res
	}

	if p, res := list(tokA, nil); res.StatusCode != http.StatusOK || p.Items == nil || len(p.Items) != 0 {
		t.Fatalf("empty list: %d %+v", res.StatusCode, p)
	}

	for _, id := range []string{"mid-popup", "ended", "tomorrow"} {
		if st := put(tokA, "test:"+id); st != http.StatusNoContent {
			t.Fatalf("put %s: %d", id, st)
		}
		*now = now.Add(time.Minute)
	}
	if st := put(tokA, "test:mid-popup"); st != http.StatusNoContent { // 멱등, saved_at 유지
		t.Fatalf("re-put: %d", st)
	}
	var e apiError
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:nope", tokA, nil, &e); res.StatusCode != http.StatusNotFound || e.Code != "not_found" {
		t.Fatalf("unknown spot: %d %q", res.StatusCode, e.Code)
	}
	if st := put(tokB, "test:too-far"); st != http.StatusNoContent {
		t.Fatalf("user B put: %d", st)
	}

	p, res := list(tokA, nil)
	if res.StatusCode != http.StatusOK || !slices.Equal(savedIDs(p), []string{"tomorrow", "ended", "mid-popup"}) {
		t.Fatalf("list: %d %v", res.StatusCode, savedIDs(p))
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", res.Header.Get("Cache-Control"))
	}
	for _, it := range p.Items {
		wantEnded := it.ID == "test:ended"
		if it.IsEnded == nil || *it.IsEnded != wantEnded || it.SavedAt == nil {
			t.Errorf("%s: is_ended=%v saved_at=%v", it.ID, it.IsEnded, it.SavedAt)
		}
	}
	if got := *p.Items[2].SavedAt; got != storetest.Now.UTC().Format(time.RFC3339) && got != storetest.Now.Format(time.RFC3339) {
		t.Errorf("mid-popup saved_at = %s, want first save time", got)
	}

	// 페이지
	p1, _ := list(tokA, url.Values{"limit": {"2"}})
	if !slices.Equal(savedIDs(p1), []string{"tomorrow", "ended"}) || p1.NextCursor == "" {
		t.Fatalf("page 1: %v %q", savedIDs(p1), p1.NextCursor)
	}
	p2, _ := list(tokA, url.Values{"limit": {"2"}, "cursor": {p1.NextCursor}})
	if !slices.Equal(savedIDs(p2), []string{"mid-popup"}) || p2.NextCursor != "" {
		t.Fatalf("page 2: %v %q", savedIDs(p2), p2.NextCursor)
	}
	for _, bad := range []url.Values{{"limit": {"0"}}, {"limit": {"101"}}, {"cursor": {"zzz"}}} {
		var e apiError
		if res := call(t, srv, http.MethodGet, "/v1/me/saved?"+bad.Encode(), tokA, nil, &e); res.StatusCode != http.StatusBadRequest || e.Code != "invalid_parameter" {
			t.Errorf("%v: %d %q", bad, res.StatusCode, e.Code)
		}
	}

	// 다른 사용자의 목록은 섞이지 않는다.
	if pb, _ := list(tokB, nil); !slices.Equal(savedIDs(pb), []string{"too-far"}) {
		t.Fatalf("user B: %v", savedIDs(pb))
	}

	// 종료 후 보존 기간(90일)이 지나면 목록에서 빠진다(수집 정리 전이라도).
	*now = storetest.Now.AddDate(0, 0, 91)
	if p, _ := list(tokA, nil); slices.Contains(savedIDs(p), "ended") {
		t.Fatalf("ended past retention still listed: %v", savedIDs(p))
	}
	*now = storetest.Now

	// 삭제(멱등)
	for range 2 {
		if res := call(t, srv, http.MethodDelete, "/v1/me/saved/test:ended", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
			t.Fatalf("delete: %d", res.StatusCode)
		}
	}
	if res := call(t, srv, http.MethodDelete, "/v1/me/saved/test:nope", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unknown: %d", res.StatusCode)
	}
	if p, _ := list(tokA, nil); !slices.Equal(savedIDs(p), []string{"tomorrow", "mid-popup"}) {
		t.Fatalf("after delete: %v", savedIDs(p))
	}
}

func TestSavedImport(t *testing.T) {
	srv, _ := meServer(t)
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:mid-popup", tokA, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal(res.StatusCode)
	}
	var r struct {
		Imported       *int `json:"imported"`
		SkippedUnknown *int `json:"skipped_unknown"`
	}
	res := call(t, srv, http.MethodPost, "/v1/me/saved:import", tokA,
		map[string]any{"spot_ids": []string{"test:mid-popup", "test:ended", "test:tomorrow", "test:gone"}}, &r)
	if res.StatusCode != http.StatusOK || r.Imported == nil || *r.Imported != 2 || r.SkippedUnknown == nil || *r.SkippedUnknown != 1 {
		t.Fatalf("import: %d %+v", res.StatusCode, r)
	}
	var p savedPage
	call(t, srv, http.MethodGet, "/v1/me/saved", tokA, nil, &p)
	if len(p.Items) != 3 {
		t.Fatalf("after import: %v", savedIDs(p))
	}

	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("test:x%d", i)
	}
	for name, body := range map[string]any{
		"not json":   "nope",
		"empty":      map[string]any{"spot_ids": []string{}},
		"missing":    map[string]any{},
		"too many":   map[string]any{"spot_ids": tooMany},
		"empty id":   map[string]any{"spot_ids": []string{""}},
		"long id":    map[string]any{"spot_ids": []string{strings.Repeat("a", 201)}},
		"wrong type": map[string]any{"spot_ids": "test:a"},
	} {
		var e apiError
		if res := call(t, srv, http.MethodPost, "/v1/me/saved:import", tokA, body, &e); res.StatusCode != http.StatusBadRequest || e.Code != "invalid_parameter" {
			t.Errorf("%s: %d %q", name, res.StatusCode, e.Code)
		}
	}
}

func TestSavedLimit(t *testing.T) {
	spots := storetest.Fixtures()
	base := spots[0]
	for i := range store.MaxSavedPerUser + 1 {
		sp := base
		sp.SourceID = fmt.Sprintf("bulk-%03d", i)
		sp.ID = "test:" + sp.SourceID
		spots = append(spots, sp)
	}
	st := memory.New(spots...)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.New(st, log, httpapi.WithAuth(testVerifier), httpapi.WithSaved(st),
		httpapi.WithClock(func() time.Time { return storetest.Now })).Handler())
	defer srv.Close()
	for i := 0; i < store.MaxSavedPerUser; i += 200 {
		var ids []string
		for j := i; j < min(i+200, store.MaxSavedPerUser); j++ {
			ids = append(ids, fmt.Sprintf("test:bulk-%03d", j))
		}
		if res := call(t, srv, http.MethodPost, "/v1/me/saved:import", tokA, map[string]any{"spot_ids": ids}, nil); res.StatusCode != http.StatusOK {
			t.Fatalf("import batch %d: %d", i, res.StatusCode)
		}
	}
	var e apiError
	res := call(t, srv, http.MethodPut, fmt.Sprintf("/v1/me/saved/test:bulk-%03d", store.MaxSavedPerUser), tokA, nil, &e)
	if res.StatusCode != http.StatusConflict || e.Code != "saved_limit_exceeded" {
		t.Fatalf("put over limit: %d %q", res.StatusCode, e.Code)
	}
	res = call(t, srv, http.MethodPost, "/v1/me/saved:import", tokA, map[string]any{"spot_ids": []string{fmt.Sprintf("test:bulk-%03d", store.MaxSavedPerUser)}}, &e)
	if res.StatusCode != http.StatusConflict || e.Code != "saved_limit_exceeded" {
		t.Fatalf("import over limit: %d %q", res.StatusCode, e.Code)
	}
}

// 실제 Supabase 검증기(가짜 JWKS 서버, ES256)를 꽂아 미들웨어가 끝까지 동작하는지 본다.
func TestSavedWithSupabaseVerifier(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		x, y := make([]byte, 32), make([]byte, 32)
		key.X.FillBytes(x)
		key.Y.FillBytes(y)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{
			{"kty": "EC", "crv": "P-256", "kid": "k1", "use": "sig", "alg": "ES256", "x": b64(x), "y": b64(y)}}})
	}))
	defer jwks.Close()
	sign := func(claims map[string]any) string {
		h, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": "k1", "typ": "JWT"})
		c, _ := json.Marshal(claims)
		signing := b64(h) + "." + b64(c)
		d := sha256.Sum256([]byte(signing))
		r, s, err := ecdsa.Sign(rand.Reader, key, d[:])
		if err != nil {
			t.Fatal(err)
		}
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return signing + "." + b64(sig)
	}
	now := time.Now()
	claims := func(exp time.Time) map[string]any {
		return map[string]any{"iss": jwks.URL + "/auth/v1", "aud": "authenticated", "sub": storetest.UserA, "exp": exp.Unix()}
	}

	var buf syncBuf
	st := memory.New(storetest.Fixtures()...)
	srv := httptest.NewServer(httpapi.New(st, slog.New(slog.NewTextHandler(&buf, nil)),
		httpapi.WithAuth(auth.NewSupabase(auth.SupabaseConfig{URL: jwks.URL})), httpapi.WithSaved(st)).Handler())
	defer srv.Close()

	good := sign(claims(now.Add(time.Hour)))
	if res := call(t, srv, http.MethodPut, "/v1/me/saved/test:mid-popup", good, nil, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d", res.StatusCode)
	}
	var p savedPage
	if res := call(t, srv, http.MethodGet, "/v1/me/saved?access_token="+good, good, nil, &p); res.StatusCode != http.StatusOK || !slices.Equal(savedIDs(p), []string{"mid-popup"}) {
		t.Fatalf("list: %d %v", res.StatusCode, savedIDs(p))
	}
	expired := sign(claims(now.Add(-time.Hour)))
	if res := call(t, srv, http.MethodGet, "/v1/me/saved", expired, nil, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired: %d", res.StatusCode)
	}
	srv.Close()
	// 토큰·사용자 ID는 로그에 남지 않는다(쿼리로 잘못 보낸 토큰도 지운다).
	out := buf.String()
	for _, secret := range []string{good, expired, strings.Split(good, ".")[2], storetest.UserA} {
		if strings.Contains(out, secret) {
			t.Fatalf("log contains secret %q:\n%s", secret[:10], out)
		}
	}
	if !strings.Contains(out, "access_token=REDACTED") {
		t.Errorf("access_token not redacted:\n%s", out)
	}
}
