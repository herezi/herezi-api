package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Apple 로그인 토큰 철회(회원 탈퇴, App Store 심사 지침 5.1.1(v), 이슈 #72).
//
// 서버는 Apple 토큰을 저장하지 않는다. 탈퇴 직전 앱이 Sign in with Apple을 다시 해서 받은 authorization code를 보내면
// ① `POST /auth/token`(grant_type=authorization_code)으로 refresh token을 받고 ② `POST /auth/revoke`로 철회한다.
// 두 요청 모두 서버가 .p8 키로 서명한 client secret(ES256 JWT)을 쓴다.

// DefaultAppleBaseURL은 Apple ID 서버.
const DefaultAppleBaseURL = "https://appleid.apple.com"

// DefaultAppleClientID는 iOS 앱 번들 ID. 앱(네이티브)이 받은 authorization code는 이 client_id로만 교환된다.
const DefaultAppleClientID = "com.herezi.app"

// ErrAppleInvalidCode는 Apple이 authorization code를 거부했을 때(invalid_grant: 만료(5분)·이미 사용·다른 앱).
// 앱이 다시 인증해 새 코드를 보내면 된다(→ 400).
var ErrAppleInvalidCode = errors.New("auth: apple: invalid authorization code")

// AppleRevoker는 authorization code로 사용자의 Apple 토큰을 철회한다.
// 실패하면 ErrAppleInvalidCode(코드 문제) 또는 그 밖의 에러(Apple 서버·네트워크 문제)를 돌려준다.
type AppleRevoker interface {
	RevokeAuthorizationCode(ctx context.Context, code string) error
}

// AppleConfig는 Sign in with Apple 키 설정. 값은 로그·에러에 넣지 않는다.
type AppleConfig struct {
	TeamID     string // client secret iss
	KeyID      string // client secret 헤더 kid
	PrivateKey string // .p8 내용(PEM, PKCS#8 EC P-256). 줄바꿈이 `\n` 글자로 들어와도 받는다.
	ClientID   string // 기본 DefaultAppleClientID
	BaseURL    string // 기본 DefaultAppleBaseURL (테스트에서 가짜 서버)
	Client     *http.Client
	Now        func() time.Time
}

// Apple은 Apple ID 서버 클라이언트.
type Apple struct {
	teamID, keyID, clientID, base string
	key                           *ecdsa.PrivateKey
	client                        *http.Client
	now                           func() time.Time
}

var _ AppleRevoker = (*Apple)(nil)

// appleSecretTTL은 client secret 수명(Apple 최대 6개월). 요청마다 새로 서명하므로 짧게 둔다.
const appleSecretTTL = 10 * time.Minute

// NewApple은 키를 읽어 클라이언트를 만든다. 키가 잘못되면 에러(키 내용은 넣지 않는다).
func NewApple(cfg AppleConfig) (*Apple, error) {
	if cfg.TeamID == "" || cfg.KeyID == "" || cfg.PrivateKey == "" {
		return nil, errors.New("auth: apple: team id, key id and private key are required")
	}
	key, err := parseAppleKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	a := &Apple{
		teamID: cfg.TeamID, keyID: cfg.KeyID, clientID: cfg.ClientID, base: strings.TrimRight(cfg.BaseURL, "/"),
		key: key, client: cfg.Client, now: cfg.Now,
	}
	if a.clientID == "" {
		a.clientID = DefaultAppleClientID
	}
	if a.base == "" {
		a.base = DefaultAppleBaseURL
	}
	if a.client == nil {
		a.client = &http.Client{Timeout: 10 * time.Second}
	}
	if a.now == nil {
		a.now = time.Now
	}
	return a, nil
}

func parseAppleKey(s string) (*ecdsa.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "\n") {
		s = strings.ReplaceAll(s, `\n`, "\n") // 환경 변수에 한 줄로 넣은 경우
	}
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("auth: apple: private key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("auth: apple: private key is not PKCS#8")
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok || ec.Curve.Params().Name != "P-256" {
		return nil, errors.New("auth: apple: private key is not EC P-256")
	}
	return ec, nil
}

// clientSecret은 ES256 JWT(iss=팀 ID, sub=client_id, aud=https://appleid.apple.com)를 만든다.
func (a *Apple) clientSecret() (string, error) {
	now := a.now()
	seg := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := seg(map[string]string{"alg": "ES256", "kid": a.keyID}) + "." + seg(map[string]any{
		"iss": a.teamID,
		"iat": now.Unix(),
		"exp": now.Add(appleSecretTTL).Unix(),
		"aud": DefaultAppleBaseURL, // 테스트 서버를 써도 aud는 Apple 고정
		"sub": a.clientID,
	})
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, digest[:])
	if err != nil {
		return "", errors.New("auth: apple: sign client secret")
	}
	sig := make([]byte, 64) // JWS ES256: r||s 각 32바이트
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

type appleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

// RevokeAuthorizationCode는 코드를 토큰으로 바꾸고 refresh token(없으면 access token)을 철회한다.
// 에러에는 코드·토큰·키·응답 본문을 넣지 않고 단계와 HTTP 상태(또는 Apple 오류 코드)만 남긴다.
func (a *Apple) RevokeAuthorizationCode(ctx context.Context, code string) error {
	if code == "" {
		return ErrAppleInvalidCode
	}
	secret, err := a.clientSecret()
	if err != nil {
		return err
	}
	status, body, err := a.post(ctx, "/auth/token", url.Values{
		"client_id":     {a.clientID},
		"client_secret": {secret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return fmt.Errorf("auth: apple token: %w", err)
	}
	var tok appleTokenResponse
	_ = json.Unmarshal(body, &tok)
	if status != http.StatusOK {
		if tok.Error == "invalid_grant" {
			return ErrAppleInvalidCode
		}
		return fmt.Errorf("auth: apple token: status %d %s", status, safeAppleError(tok.Error))
	}
	token, hint := tok.RefreshToken, "refresh_token"
	if token == "" {
		token, hint = tok.AccessToken, "access_token"
	}
	if token == "" {
		return errors.New("auth: apple token: no token in response")
	}
	status, body, err = a.post(ctx, "/auth/revoke", url.Values{
		"client_id":       {a.clientID},
		"client_secret":   {secret},
		"token":           {token},
		"token_type_hint": {hint},
	})
	if err != nil {
		return fmt.Errorf("auth: apple revoke: %w", err)
	}
	if status != http.StatusOK {
		var e appleTokenResponse
		_ = json.Unmarshal(body, &e)
		return fmt.Errorf("auth: apple revoke: status %d %s", status, safeAppleError(e.Error))
	}
	return nil
}

// safeAppleError는 Apple 오류 코드(invalid_client 등)만 남긴다. 예상 밖 값은 버린다.
func safeAppleError(code string) string {
	switch code {
	case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client", "unsupported_grant_type",
		"invalid_scope", "unsupported_token_type":
		return code
	}
	return ""
}

func (a *Apple) post(ctx context.Context, path string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, errors.New("build request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			return 0, nil, errors.New("timeout")
		}
		return 0, nil, errors.New("request failed")
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, body, nil
}
