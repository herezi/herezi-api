package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
)

// fakeApple는 Apple ID 서버(/auth/token, /auth/revoke)를 흉내 낸다. client secret 서명·클레임을 검사한다.
type fakeApple struct {
	t   *testing.T
	pub *ecdsa.PublicKey
	srv *httptest.Server

	mu           sync.Mutex
	tokenStatus  int
	tokenBody    string
	revokeStatus int
	revoked      []string // "hint:token"
	codes        []string
}

func newFakeApple(t *testing.T, pub *ecdsa.PublicKey) *fakeApple {
	f := &fakeApple{t: t, pub: pub, tokenStatus: 200,
		tokenBody:    `{"access_token":"at-1","refresh_token":"rt-1","id_token":"x","token_type":"Bearer","expires_in":3600}`,
		revokeStatus: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("client_id") != "com.herezi.app" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !f.validSecret(r.PostForm.Get("client_secret")) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		switch r.URL.Path {
		case "/auth/token":
			if r.PostForm.Get("grant_type") != "authorization_code" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.codes = append(f.codes, r.PostForm.Get("code"))
			if r.PostForm.Get("code") == "used-code" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.WriteHeader(f.tokenStatus)
			_, _ = w.Write([]byte(f.tokenBody))
		case "/auth/revoke":
			f.revoked = append(f.revoked, r.PostForm.Get("token_type_hint")+":"+r.PostForm.Get("token"))
			w.WriteHeader(f.revokeStatus)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeApple) validSecret(tok string) bool {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	var h map[string]string
	var c map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if json.Unmarshal(hb, &h) != nil || json.Unmarshal(cb, &c) != nil || len(sig) != 64 {
		return false
	}
	d := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(f.pub, d[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return false
	}
	iat, _ := c["iat"].(float64)
	exp, _ := c["exp"].(float64)
	return h["alg"] == "ES256" && h["kid"] == "KEY123" && c["iss"] == "TEAM123" && c["sub"] == "com.herezi.app" &&
		c["aud"] == "https://appleid.apple.com" && exp > iat && exp-iat <= 180*24*3600 &&
		int64(iat) == appleNow.Unix()
}

var appleNow = time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)

func p8(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func newTestApple(t *testing.T, base, key string) *auth.Apple {
	t.Helper()
	a, err := auth.NewApple(auth.AppleConfig{TeamID: "TEAM123", KeyID: "KEY123", PrivateKey: key, BaseURL: base,
		Now: func() time.Time { return appleNow }})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAppleRevoke(t *testing.T) {
	k, pemKey := p8(t)
	f := newFakeApple(t, &k.PublicKey)
	a := newTestApple(t, f.srv.URL, pemKey)
	ctx := context.Background()

	// 성공: 코드 → refresh token 철회.
	if err := a.RevokeAuthorizationCode(ctx, "code-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != "refresh_token:rt-1" {
		t.Fatalf("revoked = %v", f.revoked)
	}

	// refresh token이 없으면 access token 철회.
	f.tokenBody = `{"access_token":"at-2"}`
	if err := a.RevokeAuthorizationCode(ctx, "code-2"); err != nil || f.revoked[1] != "access_token:at-2" {
		t.Fatalf("access fallback: %v %v", err, f.revoked)
	}

	// 만료·재사용 코드(invalid_grant) → ErrAppleInvalidCode, 철회 없음.
	if err := a.RevokeAuthorizationCode(ctx, "used-code"); !errors.Is(err, auth.ErrAppleInvalidCode) {
		t.Fatalf("invalid_grant: %v", err)
	}
	if err := a.RevokeAuthorizationCode(ctx, ""); !errors.Is(err, auth.ErrAppleInvalidCode) {
		t.Fatalf("empty code: %v", err)
	}

	// 토큰 교환 서버 오류 → 다른 에러(재시도 대상), 코드·토큰은 에러에 없음.
	f.tokenStatus = http.StatusInternalServerError
	err := a.RevokeAuthorizationCode(ctx, "secret-code-3")
	if err == nil || errors.Is(err, auth.ErrAppleInvalidCode) || strings.Contains(err.Error(), "secret-code-3") {
		t.Fatalf("token 500: %v", err)
	}

	// 철회 실패 → 에러.
	f.tokenStatus, f.tokenBody, f.revokeStatus = 200, `{"refresh_token":"rt-4"}`, http.StatusServiceUnavailable
	err = a.RevokeAuthorizationCode(ctx, "code-4")
	if err == nil || errors.Is(err, auth.ErrAppleInvalidCode) || strings.Contains(err.Error(), "rt-4") {
		t.Fatalf("revoke 503: %v", err)
	}
	if want := []string{"code-1", "code-2", "used-code", "secret-code-3", "code-4"}; strings.Join(f.codes, ",") != strings.Join(want, ",") {
		t.Fatalf("codes = %v", f.codes)
	}
}

func TestAppleWrongKeyRejected(t *testing.T) {
	k, _ := p8(t)
	_, other := p8(t)
	f := newFakeApple(t, &k.PublicKey)
	a := newTestApple(t, f.srv.URL, other) // Apple에 등록되지 않은 키
	err := a.RevokeAuthorizationCode(context.Background(), "code")
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("got %v", err)
	}
}

func TestAppleUnreachable(t *testing.T) {
	_, pemKey := p8(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	err := newTestApple(t, base, pemKey).RevokeAuthorizationCode(context.Background(), "code")
	if err == nil || errors.Is(err, auth.ErrAppleInvalidCode) || strings.Contains(err.Error(), base) {
		t.Fatalf("got %v", err)
	}
}

func TestNewAppleKey(t *testing.T) {
	_, pemKey := p8(t)
	oneLine := strings.ReplaceAll(strings.TrimSpace(pemKey), "\n", `\n`)
	if _, err := auth.NewApple(auth.AppleConfig{TeamID: "T", KeyID: "K", PrivateKey: oneLine}); err != nil {
		t.Fatalf("escaped newlines: %v", err)
	}
	for name, key := range map[string]string{
		"not pem": "hello",
		"not ec":  "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n",
		"empty":   "",
	} {
		if _, err := auth.NewApple(auth.AppleConfig{TeamID: "T", KeyID: "K", PrivateKey: key}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
