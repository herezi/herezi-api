// Package auth는 Supabase Auth가 발급한 액세스 토큰(JWT)을 검증한다(TECH-PLAN.md §4.1).
//
// 앱은 카카오·Apple로 로그인한 뒤 Supabase 세션을 받고, API에는 `Authorization: Bearer <JWT>`로 보낸다.
// 서버는 프로젝트 JWKS(비대칭 키: ES256/RS256)로 서명을 확인하고, 레거시 HS256 비밀값이 설정돼 있으면
// HS256 토큰도 받는다. 표준 라이브러리만 쓴다(외부 JWT 라이브러리 없음).
//
// auth는 표준 라이브러리만 import한다. httpapi는 Verifier 인터페이스만 쓰고, 구체 구현은 cmd/api가 꽂는다.
package auth

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidToken은 토큰이 없거나 형식·서명·클레임이 맞지 않을 때(→ 401).
var ErrInvalidToken = errors.New("auth: invalid token")

// ErrUnavailable은 검증 키(JWKS)를 가져오지 못해 판단할 수 없을 때(→ 503). 토큰 잘못이 아니므로 앱은 로그아웃하지 않는다.
var ErrUnavailable = errors.New("auth: verification keys unavailable")

// User는 검증된 토큰의 주인.
type User struct {
	// ID는 Supabase 사용자 ID(`sub`, UUID 소문자).
	ID string
}

// Verifier는 액세스 토큰을 검증한다. 실패하면 ErrInvalidToken 또는 ErrUnavailable을 감싼 에러를 돌려준다.
type Verifier interface {
	Verify(ctx context.Context, token string) (User, error)
}

// BearerToken은 Authorization 헤더 값에서 토큰을 꺼낸다. "Bearer " 접두사(대소문자 무시)가 없으면 "".
func BearerToken(header string) string {
	scheme, tok, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}

// IsUUID는 s가 8-4-4-4-12 형식의 16진수 UUID인지 확인한다(Supabase 사용자 ID).
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return false
			}
		}
	}
	return true
}
