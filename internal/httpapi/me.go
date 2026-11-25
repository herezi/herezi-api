package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// /v1/me/* — 로그인한 사용자 전용(TECH-PLAN.md §4.1, 이슈 #56). 조회 API(/v1/spots 등)는 인증 없이 그대로다.

// Parameter limits from api/openapi.yaml (/v1/me/saved).
const (
	defaultSavedLimit = 50
	maxSavedLimit     = 100
	maxImportIDs      = 200
	maxSpotIDLen      = 200
	maxImportBody     = 64 << 10
)

// WithAuth sets the token verifier for /v1/me/*. 없으면 503 auth_not_configured.
func WithAuth(v auth.Verifier) Option { return func(s *Server) { s.auth = v } }

// WithSaved sets the saved-list store for /v1/me/saved*. 없으면 503 auth_not_configured.
func WithSaved(st store.SavedStore) Option { return func(s *Server) { s.saved = st } }

// authedHandler는 검증된 사용자를 받는 핸들러.
type authedHandler func(w http.ResponseWriter, r *http.Request, user auth.User)

// authed는 Authorization: Bearer <Supabase JWT>를 검증한다.
//   - 서버에 인증이 설정되지 않음(SUPABASE_URL 없음) → 503 auth_not_configured
//   - 토큰 없음·잘못됨·만료 → 401 unauthorized (+ WWW-Authenticate: Bearer)
//   - 검증 키(JWKS)를 받지 못함 → 503 auth_unavailable (앱은 로그아웃하지 않고 나중에 다시 시도)
//
// 응답은 사용자별이므로 캐시하지 않는다(Cache-Control: no-store). 토큰·사용자 ID는 로그에 남기지 않는다.
func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s.auth == nil || s.saved == nil {
			writeError(w, http.StatusServiceUnavailable, codeAuthNotConfigured, "authentication is not configured")
			return
		}
		tok := auth.BearerToken(r.Header.Get("Authorization"))
		if tok == "" {
			unauthorized(w, "missing bearer token")
			return
		}
		user, err := s.auth.Verify(r.Context(), tok)
		if errors.Is(err, auth.ErrUnavailable) {
			s.log.Warn("auth keys unavailable", "err", err.Error())
			writeError(w, http.StatusServiceUnavailable, codeAuthUnavailable, "authentication is temporarily unavailable")
			return
		}
		if err != nil {
			unauthorized(w, "invalid or expired token")
			return
		}
		h(w, r, user)
	}
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="herezi"`)
	writeError(w, http.StatusUnauthorized, codeUnauthorized, msg)
}

// listSaved: 저장 시각 최신순. 끝난 항목은 is_ended=true로 포함하고, 종료 후 보존 기간이 지난 항목은 뺀다.
func (s *Server) listSaved(w http.ResponseWriter, r *http.Request, user auth.User) {
	v := r.URL.Query()
	limit, err := parseIntParam(v.Get("limit"), "limit", defaultSavedLimit, 1, maxSavedLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	offset, err := decodeCursor(v.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	now := s.now()
	items, err := s.saved.SavedSpots(r.Context(), store.SavedQuery{
		UserID:          user.ID,
		HideEndedBefore: now.AddDate(0, 0, -domain.RetainEndedDays),
		Offset:          offset,
		Limit:           limit + 1,
	})
	if err != nil {
		s.log.Error("saved spots", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	page := spotPageJSON{Items: make([]spotJSON, 0, min(len(items), limit))}
	if len(items) > limit {
		items = items[:limit]
		page.NextCursor = encodeCursor(offset + limit)
	}
	for _, it := range items {
		j := toSpotJSON(it.Spot, now)
		at := it.SavedAt
		j.SavedAt = &at
		page.Items = append(page.Items, j)
	}
	writeJSON(w, http.StatusOK, page)
}

// putSaved: 멱등. 이미 저장돼 있어도 204(saved_at은 처음 값 유지).
func (s *Server) putSaved(w http.ResponseWriter, r *http.Request, user auth.User) {
	id := r.PathValue("spot_id")
	if id == "" || len(id) > maxSpotIDLen {
		writeError(w, http.StatusNotFound, codeNotFound, "spot not found")
		return
	}
	err := s.saved.SaveSpot(r.Context(), user.ID, id, s.now())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "spot not found")
	case errors.Is(err, store.ErrSavedLimit):
		writeError(w, http.StatusConflict, codeSavedLimit, fmt.Sprintf("you can save up to %d spots", store.MaxSavedPerUser))
	case err != nil:
		s.log.Error("save spot", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteSaved: 멱등. 저장돼 있지 않거나 없는 spot이어도 204.
func (s *Server) deleteSaved(w http.ResponseWriter, r *http.Request, user auth.User) {
	if err := s.saved.UnsaveSpot(r.Context(), user.ID, r.PathValue("spot_id")); err != nil {
		s.log.Error("unsave spot", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// importSavedJSON is components.schemas.SavedImportRequest.
type importSavedJSON struct {
	SpotIDs []string `json:"spot_ids"`
}

// importResultJSON is components.schemas.SavedImportResult.
type importResultJSON struct {
	Imported       int `json:"imported"`
	SkippedUnknown int `json:"skipped_unknown"`
}

// importSaved: 로그인 전 기기 로컬 저장분을 한 번에 옮긴다. 없는 spot(정리됨)은 건너뛰고 센다.
func (s *Server) importSaved(w http.ResponseWriter, r *http.Request, user auth.User) {
	var body importSavedJSON
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxImportBody))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "body must be JSON: {\"spot_ids\": [...]}")
		return
	}
	if n := len(body.SpotIDs); n < 1 || n > maxImportIDs {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, fmt.Sprintf("spot_ids must have 1 to %d items", maxImportIDs))
		return
	}
	for _, id := range body.SpotIDs {
		if id == "" || len(id) > maxSpotIDLen {
			writeError(w, http.StatusBadRequest, codeInvalidParameter, fmt.Sprintf("each spot_id must be 1 to %d bytes", maxSpotIDLen))
			return
		}
	}
	res, err := s.saved.ImportSaved(r.Context(), user.ID, body.SpotIDs, s.now())
	switch {
	case errors.Is(err, store.ErrSavedLimit):
		writeError(w, http.StatusConflict, codeSavedLimit, fmt.Sprintf("you can save up to %d spots", store.MaxSavedPerUser))
	case err != nil:
		s.log.Error("import saved", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
	default:
		writeJSON(w, http.StatusOK, importResultJSON{Imported: res.Imported, SkippedUnknown: res.SkippedUnknown})
	}
}

// WithUserDeleter sets the auth-server user deleter for DELETE /v1/me. 없으면 503 auth_not_configured.
func WithUserDeleter(d auth.UserDeleter) Option { return func(s *Server) { s.users = d } }

// WithAppleRevoker sets the Sign in with Apple token revoker for DELETE /v1/me (이슈 #72).
// 없으면 Apple 사용자의 탈퇴는 503 auth_not_configured(철회 없이 지우지 않는다).
func WithAppleRevoker(a auth.AppleRevoker) Option { return func(s *Server) { s.apple = a } }

// maxDeleteMeBody는 DELETE /v1/me 본문 상한({"apple_authorization_code": "..."}).
const (
	maxDeleteMeBody      = 8 << 10
	maxAppleCodeLen      = 2048
	appleReauthMessage   = "sign in with Apple again and send apple_authorization_code"
	appleRevokeFailedMsg = "failed to revoke Apple token"
)

type deleteMeRequest struct {
	AppleAuthorizationCode string `json:"apple_authorization_code"`
}

// deleteMe는 회원 탈퇴다(S6, App Store 심사 지침 5.1.1(v)).
//
// 순서: ⓪ Apple 사용자면 Apple 토큰 철회 → ① Supabase Admin API로 사용자 삭제 → ② 성공하면 저장 행 삭제 → 204.
//   - ⓪ Apple 제공자가 연결된 사용자는 본문 `apple_authorization_code`(탈퇴 직전 재인증으로 받은 코드)가 필요하다.
//     없거나 Apple이 거부하면(만료·재사용) 400 apple_reauth_required, Apple 서버 오류면 502 upstream_error.
//     어느 쪽이든 아무것도 지우지 않는다 — 앱이 다시 인증해 재시도한다. 다른 제공자 사용자는 본문을 무시한다.
//   - ①이 실패하면 502 upstream_error, 저장 행은 그대로 둔다(계정이 남았는데 저장만 사라지지 않게). 앱은 재시도한다.
//   - ①은 성공했는데 ②가 실패하면 500. 액세스 토큰은 만료 전까지 유효하므로 앱이 재시도하면
//     ①은 이미 없는 사용자(404)라 성공으로 넘어가고 ②를 다시 한다(멱등).
func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.users == nil {
		writeError(w, http.StatusServiceUnavailable, codeAuthNotConfigured, "account deletion is not configured")
		return
	}
	var body deleteMeRequest
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxDeleteMeBody+1))
	if err != nil || len(raw) > maxDeleteMeBody {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "request body is too large or unreadable")
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil || len(body.AppleAuthorizationCode) > maxAppleCodeLen {
			writeError(w, http.StatusBadRequest, codeInvalidParameter, "invalid request body")
			return
		}
	}
	if user.HasProvider(auth.ProviderApple) {
		if s.apple == nil {
			s.log.Error("apple revoke not configured") // APPLE_TEAM_ID·APPLE_KEY_ID·APPLE_PRIVATE_KEY
			writeError(w, http.StatusServiceUnavailable, codeAuthNotConfigured, "Apple account deletion is not configured")
			return
		}
		if body.AppleAuthorizationCode == "" {
			writeError(w, http.StatusBadRequest, codeAppleReauth, appleReauthMessage)
			return
		}
		err := s.apple.RevokeAuthorizationCode(r.Context(), body.AppleAuthorizationCode)
		switch {
		case errors.Is(err, auth.ErrAppleInvalidCode):
			writeError(w, http.StatusBadRequest, codeAppleReauth, appleReauthMessage)
			return
		case err != nil:
			s.log.Error("apple revoke", "err", logErr(err)) // 코드·토큰·사용자 ID는 남기지 않는다
			writeError(w, http.StatusBadGateway, codeUpstream, appleRevokeFailedMsg)
			return
		}
	}
	if err := s.users.DeleteUser(r.Context(), user.ID); err != nil {
		s.log.Error("delete auth user", "err", logErr(err)) // 사용자 ID·키는 남기지 않는다
		writeError(w, http.StatusBadGateway, codeUpstream, "failed to delete account")
		return
	}
	if _, err := s.saved.DeleteUserSaved(r.Context(), user.ID); err != nil {
		s.log.Error("delete user saved", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
