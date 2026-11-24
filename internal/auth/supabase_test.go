package auth_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
)

const userID = "8f14e45f-ceea-467a-9f3b-1c2d3e4f5a6b"

var testNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// fakeSupabase는 JWKS를 내주는 가짜 Supabase 서버와 서명 키를 가진다.
type fakeSupabase struct {
	srv      *httptest.Server
	ec       *ecdsa.PrivateKey
	rsa      *rsa.PrivateKey
	jwksHits atomic.Int32
	fail     atomic.Bool
	kids     atomic.Value // []string: JWKS에 넣을 EC 키 kid
}

func newFakeSupabase(t *testing.T) *fakeSupabase {
	t.Helper()
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSupabase{ec: ec, rsa: rk}
	f.kids.Store([]string{"ec-1"})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		f.jwksHits.Add(1)
		if f.fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		b64 := base64.RawURLEncoding.EncodeToString
		var keys []map[string]string
		for _, kid := range f.kids.Load().([]string) {
			x, y := make([]byte, 32), make([]byte, 32)
			f.ec.X.FillBytes(x)
			f.ec.Y.FillBytes(y)
			keys = append(keys, map[string]string{"kty": "EC", "crv": "P-256", "kid": kid, "use": "sig", "alg": "ES256", "x": b64(x), "y": b64(y)})
		}
		keys = append(keys, map[string]string{"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256",
			"n": b64(f.rsa.N.Bytes()), "e": b64(big.NewInt(int64(f.rsa.E)).Bytes())})
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSupabase) issuer() string { return f.srv.URL + "/auth/v1" }

func validClaims(f *fakeSupabase) map[string]any {
	return map[string]any{
		"iss": f.issuer(), "sub": userID, "aud": "authenticated", "role": "authenticated",
		"exp": testNow.Add(time.Hour).Unix(), "iat": testNow.Add(-time.Minute).Unix(),
	}
}

func seg(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign은 alg에 맞는 키로 토큰을 만든다. HS256은 secret을 쓴다.
func (f *fakeSupabase) sign(t *testing.T, alg, kid string, claims map[string]any, secret string) string {
	t.Helper()
	h := map[string]any{"alg": alg, "typ": "JWT"}
	if kid != "" {
		h["kid"] = kid
	}
	signing := seg(h) + "." + seg(claims)
	digest := sha256.Sum256([]byte(signing))
	var sig []byte
	switch alg {
	case "ES256":
		r, s, err := ecdsa.Sign(rand.Reader, f.ec, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
	case "RS256":
		var err error
		if sig, err = rsa.SignPKCS1v15(rand.Reader, f.rsa, crypto.SHA256, digest[:]); err != nil {
			t.Fatal(err)
		}
	case "HS256":
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(signing))
		sig = m.Sum(nil)
	case "none":
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newVerifier(f *fakeSupabase, secret string, now *time.Time) *auth.Supabase {
	return auth.NewSupabase(auth.SupabaseConfig{
		URL: f.srv.URL + "/", JWTSecret: secret,
		Now: func() time.Time { return *now },
	})
}

func TestVerify(t *testing.T) {
	f := newFakeSupabase(t)
	const secret = "legacy-hs256-secret-for-tests-only"
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	with := func(mod func(c map[string]any)) map[string]any {
		c := validClaims(f)
		mod(c)
		return c
	}
	cases := []struct {
		name   string
		token  func() string
		secret string
		ok     bool
	}{
		{"ES256 valid", func() string { return f.sign(t, "ES256", "ec-1", validClaims(f), "") }, "", true},
		{"RS256 valid", func() string { return f.sign(t, "RS256", "rsa-1", validClaims(f), "") }, "", true},
		{"aud array", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["aud"] = []string{"x", "authenticated"} }), "")
		}, "", true},
		{"HS256 with secret", func() string { return f.sign(t, "HS256", "", validClaims(f), secret) }, secret, true},
		{"HS256 without configured secret", func() string { return f.sign(t, "HS256", "", validClaims(f), secret) }, "", false},
		{"HS256 wrong secret", func() string { return f.sign(t, "HS256", "", validClaims(f), "other") }, secret, false},
		{"expired", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["exp"] = testNow.Add(-time.Minute).Unix() }), "")
		}, "", false},
		{"within clock skew", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["exp"] = testNow.Add(-10 * time.Second).Unix() }), "")
		}, "", true},
		{"no exp", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { delete(c, "exp") }), "")
		}, "", false},
		{"not yet valid", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["nbf"] = testNow.Add(time.Hour).Unix() }), "")
		}, "", false},
		{"wrong issuer", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["iss"] = "https://other.supabase.co/auth/v1" }), "")
		}, "", false},
		{"wrong audience", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["aud"] = "anon" }), "")
		}, "", false},
		{"sub not uuid", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["sub"] = "kakao|123" }), "")
		}, "", false},
		{"anonymous user", func() string {
			return f.sign(t, "ES256", "ec-1", with(func(c map[string]any) { c["is_anonymous"] = true }), "")
		}, "", false},
		{"tampered claims", func() string {
			tok := f.sign(t, "ES256", "ec-1", validClaims(f), "")
			p := strings.Split(tok, ".")
			p[1] = seg(with(func(c map[string]any) { c["sub"] = "00000000-0000-4000-8000-000000000000" }))
			return strings.Join(p, ".")
		}, "", false},
		{"signed by unknown key", func() string {
			g := &fakeSupabase{ec: other, rsa: f.rsa}
			return g.sign(t, "ES256", "ec-1", validClaims(f), "")
		}, "", false},
		{"alg none", func() string { return f.sign(t, "none", "ec-1", validClaims(f), "") }, secret, false},
		{"ES256 header with RSA kid", func() string { return f.sign(t, "ES256", "rsa-1", validClaims(f), "") }, "", false},
		{"unknown kid", func() string { return f.sign(t, "ES256", "nope", validClaims(f), "") }, "", false},
		{"empty", func() string { return "" }, "", false},
		{"garbage", func() string { return "abc.def" }, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := testNow
			v := newVerifier(f, tc.secret, &now)
			u, err := v.Verify(context.Background(), tc.token())
			if tc.ok {
				if err != nil || u.ID != userID {
					t.Fatalf("got %+v, %v", u, err)
				}
				return
			}
			if !errors.Is(err, auth.ErrInvalidToken) {
				t.Fatalf("err = %v, want ErrInvalidToken", err)
			}
			if err != nil && strings.Contains(err.Error(), tc.token()) && tc.token() != "" {
				t.Fatal("error must not contain the token")
			}
		})
	}
}

func TestJWKSCacheAndRefresh(t *testing.T) {
	f := newFakeSupabase(t)
	now := testNow
	v := newVerifier(f, "", &now)
	ctx := context.Background()
	verify := func(kid string) error {
		c := validClaims(f)
		c["exp"] = now.Add(time.Hour).Unix()
		_, err := v.Verify(ctx, f.sign(t, "ES256", kid, c, ""))
		return err
	}
	for range 3 {
		if err := verify("ec-1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.jwksHits.Load(); n != 1 {
		t.Fatalf("jwks fetched %d times, want 1 (cached)", n)
	}

	// 키 교체: 모르는 kid는 1분 간격 안에서는 다시 받지 않는다.
	f.kids.Store([]string{"ec-1", "ec-2"})
	if err := verify("ec-2"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want invalid before refresh interval", err)
	}
	now = now.Add(2 * time.Minute)
	if err := verify("ec-2"); err != nil {
		t.Fatalf("after refresh interval: %v", err)
	}
	if n := f.jwksHits.Load(); n != 2 {
		t.Fatalf("jwks fetched %d times, want 2", n)
	}

	// TTL이 지나 다시 받다가 실패해도 이전 키로 검증한다.
	f.fail.Store(true)
	now = now.Add(11 * time.Minute)
	if err := verify("ec-1"); err != nil {
		t.Fatalf("stale cache should be used when refresh fails: %v", err)
	}
	if n := f.jwksHits.Load(); n != 3 {
		t.Fatalf("jwks fetched %d times, want 3", n)
	}
}

func TestJWKSUnavailable(t *testing.T) {
	f := newFakeSupabase(t)
	f.fail.Store(true)
	now := testNow
	v := newVerifier(f, "", &now)
	_, err := v.Verify(context.Background(), f.sign(t, "ES256", "ec-1", validClaims(f), ""))
	if !errors.Is(err, auth.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), f.srv.URL) {
		t.Fatalf("error must not contain URL: %v", err)
	}
}

func TestBearerToken(t *testing.T) {
	for in, want := range map[string]string{
		"Bearer abc":   "abc",
		"bearer  abc ": "abc",
		"Basic abc":    "",
		"abc":          "",
		"":             "",
	} {
		if got := auth.BearerToken(in); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsUUID(t *testing.T) {
	for in, want := range map[string]bool{
		userID:                                 true,
		strings.ToUpper(userID):                true,
		"8f14e45f-ceea-467a-9f3b-1c2d3e4f5a6":  false,
		"8f14e45fxceea-467a-9f3b-1c2d3e4f5a6b": false,
		"8f14e45f-ceea-467a-9f3b-1c2d3e4f5a6g": false,
	} {
		if got := auth.IsUUID(in); got != want {
			t.Errorf("IsUUID(%q) = %v", in, got)
		}
	}
}
