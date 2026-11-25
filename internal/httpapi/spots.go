package httpapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// cacheControl is sent on successful spot detail responses (TECH-PLAN.md §4).
const cacheControl = "public, max-age=60"

// spotJSON is components.schemas.Spot.
type spotJSON struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary,omitempty"`
	Category  domain.Category `json:"category"`
	Lat       float64         `json:"lat"`
	Lng       float64         `json:"lng"`
	Address   string          `json:"address,omitempty"`
	PlaceName string          `json:"place_name,omitempty"`
	StartsAt  time.Time       `json:"starts_at"`
	EndsAt    *time.Time      `json:"ends_at,omitempty"`
	IsFree    *bool           `json:"is_free,omitempty"`
	ImageURL  string          `json:"image_url,omitempty"`
	Source    string          `json:"source"`
	SourceURL string          `json:"source_url,omitempty"`
	FetchedAt time.Time       `json:"fetched_at"`
	DistanceM *int            `json:"distance_m,omitempty"`
	IsEnded   bool            `json:"is_ended"`
	SavedAt   *time.Time      `json:"saved_at,omitempty"` // /v1/me/saved에서만
}

// spotPageJSON is components.schemas.SpotPage.
type spotPageJSON struct {
	Items      []spotJSON `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// toSpotJSON converts a spot; is_ended is computed against now (the request clock).
func toSpotJSON(s domain.Spot, now time.Time) spotJSON {
	return spotJSON{
		ID: s.ID, Title: s.Title, Summary: s.Summary, Category: s.Category,
		Lat: s.Lat, Lng: s.Lng, Address: s.Address, PlaceName: s.PlaceName,
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, IsFree: s.IsFree, ImageURL: s.ImageURL,
		Source: s.Source, SourceURL: s.SourceURL, FetchedAt: s.FetchedAt, DistanceM: s.DistanceM,
		IsEnded: s.Ended(now),
	}
}

func (s *Server) getSpot(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	sp, err := s.spots.SpotByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "spot not found")
		return
	}
	if err != nil {
		s.log.Error("get spot", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	w.Header().Set("Cache-Control", cacheControl)
	writeJSON(w, http.StatusOK, toSpotJSON(sp, now))
}

func parseIntParam(raw, name string, def, lo, hi int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer in [%d, %d]", name, lo, hi)
	}
	return n, nil
}

// Cursors are opaque to clients. 지금은 오프셋 기반이라 페이지 사이에 데이터가 바뀌면
// 항목이 겹치거나 빠질 수 있다. 필요해지면 (distance, id) 키셋 커서로 바꾼다.
const cursorPrefix = "o:"

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.Itoa(offset)))
}

func decodeCursor(c string) (int, error) {
	if c == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil {
		if rest, ok := strings.CutPrefix(string(b), cursorPrefix); ok {
			if n, err := strconv.Atoi(rest); err == nil && n >= 0 {
				return n, nil
			}
		}
	}
	return 0, errors.New("cursor is invalid; pass next_cursor from a previous response unchanged")
}
