package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
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
