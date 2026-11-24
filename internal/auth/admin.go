package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserDeleter는 인증 서버에서 사용자를 지운다(회원 탈퇴, App Store 심사 지침 5.1.1(v)).
// 이미 없는 사용자면 nil을 돌려준다(멱등 — 탈퇴 재시도).
type UserDeleter interface {
	DeleteUser(ctx context.Context, userID string) error
}

// SupabaseAdmin은 Supabase Auth Admin API 클라이언트. 서비스 역할 키(service_role 또는 sb_secret_…)가 필요하다.
// 키는 서버에만 두고 에러·로그에 넣지 않는다.
type SupabaseAdmin struct {
	base   string // <SUPABASE_URL>/auth/v1
	key    string
	client *http.Client
}

var _ UserDeleter = (*SupabaseAdmin)(nil)

// NewSupabaseAdmin은 클라이언트를 만든다. client가 nil이면 타임아웃 10초.
func NewSupabaseAdmin(projectURL, serviceKey string, client *http.Client) *SupabaseAdmin {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SupabaseAdmin{base: Issuer(projectURL), key: serviceKey, client: client}
}

// DeleteUser는 `DELETE /auth/v1/admin/users/{id}`를 부른다(하드 삭제 — 연결된 카카오·Apple identity도 지워진다).
// 404(이미 없음)는 성공으로 본다. 에러에는 URL·키·응답 본문을 넣지 않고 HTTP 상태만 남긴다.
func (a *SupabaseAdmin) DeleteUser(ctx context.Context, userID string) error {
	if !IsUUID(userID) {
		return errors.New("auth: admin delete: invalid user id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		a.base+"/admin/users/"+url.PathEscape(strings.ToLower(userID)),
		strings.NewReader(`{"should_soft_delete":false}`))
	if err != nil {
		return errors.New("auth: admin delete: build request")
	}
	req.Header.Set("apikey", a.key)
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			return errors.New("auth: admin delete: timeout")
		}
		return errors.New("auth: admin delete: request failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300, res.StatusCode == http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("auth: admin delete: status %d", res.StatusCode)
}
