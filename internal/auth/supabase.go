package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Supabase 토큰 규칙.
const (
	// Audience는 로그인한 사용자 토큰의 aud.
	Audience = "authenticated"
	// clockSkew는 exp·nbf·iat 비교 때 허용하는 시계 오차.
	clockSkew = 30 * time.Second
	// jwksTTL이 지나면 다음 검증 때 JWKS를 다시 받는다(키 교체 반영).
	jwksTTL = 10 * time.Minute
	// jwksMinRefresh: 모르는 kid가 와도 이 간격보다 자주 JWKS를 다시 받지 않는다(남용 방지).
	jwksMinRefresh = time.Minute
	// maxTokenBytes보다 긴 Authorization 값은 보지 않는다.
	maxTokenBytes = 8 << 10
)

// SupabaseConfig는 Supabase 프로젝트 정보.
type SupabaseConfig struct {
	// URL은 프로젝트 URL(예: https://<ref>.supabase.co). 끝의 /는 무시한다.
	URL string
	// JWTSecret은 레거시 HS256 비밀값. 비어 있으면 HS256 토큰은 거절한다.
	JWTSecret string
	// HTTPClient는 JWKS를 받을 때 쓴다. nil이면 타임아웃 5초 기본 클라이언트.
	HTTPClient *http.Client
	// Now는 시계(테스트). nil이면 time.Now.
	Now func() time.Time
}

// Issuer는 프로젝트 URL로 만든 토큰 iss(`<URL>/auth/v1`).
func Issuer(projectURL string) string { return strings.TrimRight(projectURL, "/") + "/auth/v1" }

// Supabase는 Supabase Auth 토큰 검증기. 여러 고루틴에서 같이 써도 된다.
type Supabase struct {
	issuer  string
	jwksURL string
	secret  []byte
	client  *http.Client
	now     func() time.Time

	mu        sync.Mutex
	keys      map[string]crypto.PublicKey // kid → *ecdsa.PublicKey | *rsa.PublicKey
	fetchedAt time.Time                   // 마지막으로 JWKS를 받으려고 시도한 시각
	loaded    bool                        // 한 번이라도 성공했는지
}

var _ Verifier = (*Supabase)(nil)

// NewSupabase는 검증기를 만든다. JWKS는 처음 검증할 때 받는다.
func NewSupabase(cfg SupabaseConfig) *Supabase {
	s := &Supabase{
		issuer: Issuer(cfg.URL),
		client: cfg.HTTPClient,
		now:    cfg.Now,
	}
	s.jwksURL = s.issuer + "/.well-known/jwks.json"
	if cfg.JWTSecret != "" {
		s.secret = []byte(cfg.JWTSecret)
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 5 * time.Second}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Iss         string          `json:"iss"`
	Sub         string          `json:"sub"`
	Aud         json.RawMessage `json:"aud"`
	Exp         *json.Number    `json:"exp"`
	Nbf         *json.Number    `json:"nbf"`
	IsAnonymous bool            `json:"is_anonymous"`
	AppMetadata struct {
		Provider  string   `json:"provider"`
		Providers []string `json:"providers"`
	} `json:"app_metadata"`
}

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidToken, reason) }

// Verify는 서명·iss·aud·exp(·nbf)·sub를 확인한다. 에러 메시지에 토큰 내용은 넣지 않는다.
func (s *Supabase) Verify(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, invalid("missing")
	}
	if len(token) > maxTokenBytes {
		return User{}, invalid("too long")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return User{}, invalid("malformed")
	}
	var h jwtHeader
	if err := decodeSegment(parts[0], &h); err != nil {
		return User{}, invalid("bad header")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return User{}, invalid("bad signature encoding")
	}
	signed := []byte(parts[0] + "." + parts[1])
	if err := s.verifySignature(ctx, h, signed, sig); err != nil {
		return User{}, err
	}
	var c jwtClaims
	if err := decodeSegment(parts[1], &c); err != nil {
		return User{}, invalid("bad claims")
	}
	if err := s.checkClaims(c); err != nil {
		return User{}, err
	}
	return User{ID: strings.ToLower(c.Sub), Providers: providers(c.AppMetadata.Provider, c.AppMetadata.Providers)}, nil
}

// providers는 app_metadata.providers(연결된 모든 제공자)와 provider(처음 가입한 제공자)를 합친다.
func providers(first string, all []string) []string {
	var out []string
	for _, p := range append([]string{first}, all...) {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	return d.Decode(v)
}

func (s *Supabase) verifySignature(ctx context.Context, h jwtHeader, signed, sig []byte) error {
	switch h.Alg {
	case "HS256":
		if s.secret == nil {
			return invalid("HS256 not accepted")
		}
		m := hmac.New(sha256.New, s.secret)
		m.Write(signed)
		if !hmac.Equal(m.Sum(nil), sig) {
			return invalid("signature mismatch")
		}
		return nil
	case "ES256", "RS256":
		key, err := s.key(ctx, h.Kid)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(signed)
		switch k := key.(type) {
		case *ecdsa.PublicKey:
			if h.Alg != "ES256" || len(sig) != 64 {
				return invalid("alg/key mismatch")
			}
			r, ss := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
			if !ecdsa.Verify(k, digest[:], r, ss) {
				return invalid("signature mismatch")
			}
			return nil
		case *rsa.PublicKey:
			if h.Alg != "RS256" {
				return invalid("alg/key mismatch")
			}
			if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
				return invalid("signature mismatch")
			}
			return nil
		}
		return invalid("unsupported key")
	}
	return invalid("unsupported alg") // none, HS384 등
}

func (s *Supabase) checkClaims(c jwtClaims) error {
	now := s.now()
	if c.Iss != s.issuer {
		return invalid("issuer")
	}
	if !audContains(c.Aud, Audience) {
		return invalid("audience")
	}
	if c.Exp == nil {
		return invalid("no exp")
	}
	exp, err := c.Exp.Float64()
	if err != nil || now.Add(-clockSkew).After(unix(exp)) {
		return invalid("expired")
	}
	if c.Nbf != nil {
		nbf, err := c.Nbf.Float64()
		if err != nil || now.Add(clockSkew).Before(unix(nbf)) {
			return invalid("not yet valid")
		}
	}
	if !IsUUID(c.Sub) {
		return invalid("subject")
	}
	if c.IsAnonymous {
		return invalid("anonymous user") // 익명 로그인은 쓰지 않는다(카카오·Apple만)
	}
	return nil
}

func unix(sec float64) time.Time { return time.Unix(int64(sec), 0) }

// audContains: aud는 문자열 하나이거나 문자열 배열이다.
func audContains(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return slices.Contains(many, want)
	}
	return false
}

// key는 kid에 맞는 공개키를 돌려준다. 캐시가 없거나 오래됐으면 JWKS를 받고,
// 모르는 kid면(키 교체 직후) jwksMinRefresh 간격으로 다시 받는다.
// 받기에 실패해도 이전에 받은 키가 있으면 그것으로 검증한다.
func (s *Supabase) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	stale := !s.loaded || now.Sub(s.fetchedAt) >= jwksTTL
	k, ok := s.lookup(kid)
	if ok && !stale {
		return k, nil
	}
	if stale || now.Sub(s.fetchedAt) >= jwksMinRefresh {
		s.fetchedAt = now
		keys, err := s.fetchJWKS(ctx)
		if err == nil {
			s.keys, s.loaded = keys, true
			k, ok = s.lookup(kid)
		} else if !s.loaded {
			return nil, err
		}
	}
	if !ok {
		return nil, invalid("unknown key")
	}
	return k, nil
}

// lookup: kid가 비어 있으면 키가 하나뿐일 때만 그 키를 쓴다.
func (s *Supabase) lookup(kid string) (crypto.PublicKey, bool) {
	if kid == "" {
		if len(s.keys) == 1 {
			for _, k := range s.keys {
				return k, true
			}
		}
		return nil, false
	}
	k, ok := s.keys[kid]
	return k, ok
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchJWKS는 JWKS를 받아 서명용 EC(P-256)·RSA 공개키만 고른다. 에러에 URL을 넣지 않는다.
func (s *Supabase) fetchJWKS(ctx context.Context) (map[string]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: jwks request failed", ErrUnavailable)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: jwks status %d", ErrUnavailable, res.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("%w: jwks decode", ErrUnavailable)
	}
	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if pk, err := k.publicKey(); err == nil {
			keys[k.Kid] = pk
		}
	}
	return keys, nil
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	b64 := base64.RawURLEncoding.DecodeString
	switch k.Kty {
	case "EC":
		if k.Crv != "P-256" {
			return nil, errors.New("unsupported curve")
		}
		x, err1 := b64(k.X)
		y, err2 := b64(k.Y)
		if err1 != nil || err2 != nil || len(x) != 32 || len(y) != 32 {
			return nil, errors.New("bad EC key")
		}
		// 압축 안 된 점(0x04||X||Y)으로 곡선 위의 점인지 확인한다.
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return nil, err
		}
		return pub, nil
	case "RSA":
		n, err1 := b64(k.N)
		e, err2 := b64(k.E)
		if err1 != nil || err2 != nil || len(n) < 256 || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("bad RSA key")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
	}
	return nil, errors.New("unsupported kty")
}
