package httpapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// Parameter limits from api/openapi.yaml (/v1/spots).
const (
	defaultRadiusM = 3000
	minRadiusM     = 100
	maxRadiusM     = 20000
	defaultLimit   = 30
	maxLimit       = 100
	// /v1/spots/map
	defaultMapLimit = 200
	maxMapLimit     = 500
	// q (키워드 검색): 앞뒤 공백을 자른 뒤 글자(rune) 수
	minQueryRunes = 1
	maxQueryRunes = 50
)

// cacheControl is sent on successful spot responses (TECH-PLAN.md §4).
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
}

// spotPageJSON is components.schemas.SpotPage.
type spotPageJSON struct {
	Items      []spotJSON `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// spotMapJSON is components.schemas.SpotMap.
type spotMapJSON struct {
	Items     []spotJSON `json:"items"`
	Truncated bool       `json:"truncated"`
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

func (s *Server) listSpots(w http.ResponseWriter, r *http.Request) {
	q, err := s.parseListQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	limit := q.Limit
	q.Limit = limit + 1 // fetch one extra to know whether there is a next page
	spots, err := s.spots.NearbySpots(r.Context(), q)
	if err != nil {
		s.log.Error("nearby spots", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	page := spotPageJSON{Items: make([]spotJSON, 0, min(len(spots), limit))}
	if len(spots) > limit {
		spots = spots[:limit]
		page.NextCursor = encodeCursor(q.Offset + limit)
	}
	for _, sp := range spots {
		page.Items = append(page.Items, toSpotJSON(sp, q.Now))
	}
	w.Header().Set("Cache-Control", cacheControl)
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) listSpotsInBBox(w http.ResponseWriter, r *http.Request) {
	q, err := s.parseBBoxQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	limit := q.Limit
	q.Limit = limit + 1 // fetch one extra to know whether the result was truncated
	spots, err := s.spots.SpotsInBBox(r.Context(), q)
	if err != nil {
		s.log.Error("spots in bbox", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	res := spotMapJSON{Items: make([]spotJSON, 0, min(len(spots), limit))}
	if len(spots) > limit {
		spots = spots[:limit]
		res.Truncated = true
	}
	for _, sp := range spots {
		res.Items = append(res.Items, toSpotJSON(sp, q.Now))
	}
	w.Header().Set("Cache-Control", cacheControl)
	writeJSON(w, http.StatusOK, res)
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

func (s *Server) parseListQuery(r *http.Request) (store.NearbyQuery, error) {
	v := r.URL.Query()
	var q store.NearbyQuery
	var err error
	if q.Lat, err = parseCoord(v.Get("lat"), "lat", 90); err != nil {
		return q, err
	}
	if q.Lng, err = parseCoord(v.Get("lng"), "lng", 180); err != nil {
		return q, err
	}
	if q.RadiusM, err = parseIntParam(v.Get("radius_m"), "radius_m", defaultRadiusM, minRadiusM, maxRadiusM); err != nil {
		return q, err
	}
	if q.Limit, err = parseIntParam(v.Get("limit"), "limit", defaultLimit, 1, maxLimit); err != nil {
		return q, err
	}
	if q.SpotFilter, err = s.parseFilter(v); err != nil {
		return q, err
	}
	if q.Offset, err = decodeCursor(v.Get("cursor")); err != nil {
		return q, err
	}
	if v.Has("q") {
		if q.Keyword, err = parseKeyword(v.Get("q")); err != nil {
			return q, err
		}
	}
	return q, nil
}

// parseKeyword trims q and checks its length in runes (한글 한 글자 = 1).
func parseKeyword(raw string) (string, error) {
	kw := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(kw); n < minQueryRunes || n > maxQueryRunes {
		return "", fmt.Errorf("q must be %d to %d characters after trimming whitespace", minQueryRunes, maxQueryRunes)
	}
	return kw, nil
}

func (s *Server) parseBBoxQuery(r *http.Request) (store.BBoxQuery, error) {
	v := r.URL.Query()
	var q store.BBoxQuery
	var err error
	for _, c := range []struct {
		dst    *float64
		name   string
		absMax float64
	}{
		{&q.SwLat, "sw_lat", 90}, {&q.SwLng, "sw_lng", 180},
		{&q.NeLat, "ne_lat", 90}, {&q.NeLng, "ne_lng", 180},
	} {
		if *c.dst, err = parseCoord(v.Get(c.name), c.name, c.absMax); err != nil {
			return q, err
		}
	}
	if err = q.Validate(); err != nil {
		return q, err
	}
	if q.Limit, err = parseIntParam(v.Get("limit"), "limit", defaultMapLimit, 1, maxMapLimit); err != nil {
		return q, err
	}
	if q.SpotFilter, err = s.parseFilter(v); err != nil {
		return q, err
	}
	return q, nil
}

// parseFilter parses category, when and free, shared by /v1/spots and /v1/spots/map.
func (s *Server) parseFilter(v url.Values) (store.SpotFilter, error) {
	var f store.SpotFilter
	var err error
	if f.Categories, err = domain.ParseCategories(v.Get("category")); err != nil {
		return f, fmt.Errorf("category: %w", err)
	}
	when, err := domain.ParseWhen(v.Get("when"))
	if err != nil {
		return f, err
	}
	if f.FreeOnly, err = parseBoolParam(v.Get("free"), "free"); err != nil {
		return f, err
	}
	f.Now = s.now()
	f.From, f.To = when.Window(f.Now)
	return f, nil
}

func parseCoord(raw, name string, absMax float64) (float64, error) {
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(f) || math.Abs(f) > absMax {
		return 0, fmt.Errorf("%s must be a number in [-%g, %g]", name, absMax, absMax)
	}
	return f, nil
}

// parseBoolParam accepts only "true" / "false" (empty = false), as in openapi boolean.
func parseBoolParam(raw, name string) (bool, error) {
	switch raw {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
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
