// Package store defines persistence interfaces for spots.
// Implementations: store/memory (tests, local fake) and store/postgres (PostGIS + GORM).
// store may import only internal/domain (see ARCHITECTURE.md).
// 모든 구현체는 store/storetest 스위트를 통과해야 한다.
package store

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// ErrNotFound is returned by SpotByID when no spot exists.
var ErrNotFound = errors.New("store: not found")

// SpotFilter is shared by list queries (목록·지도). 상세(SpotByID)에는 적용하지 않는다.
//
// 구현체는 반드시 다음을 지킨다:
//   - ends_at < Now 인 항목은 반환하지 않는다 (끝난 행사 숨김).
//   - 기간 [From, To]와 겹치는 항목만 (domain.Spot.Overlaps).
//   - Categories가 비어 있으면 전체, 아니면 그중 하나.
//   - FreeOnly면 IsFree가 true인 항목만 (nil=모름은 제외).
type SpotFilter struct {
	Categories []domain.Category // empty = all
	FreeOnly   bool
	From, To   time.Time
	Now        time.Time
}

// Match reports whether sp passes the filter (reference semantics for every store).
func (f SpotFilter) Match(sp domain.Spot) bool {
	if sp.Ended(f.Now) || !sp.Overlaps(f.From, f.To) {
		return false
	}
	if len(f.Categories) > 0 && !slices.Contains(f.Categories, sp.Category) {
		return false
	}
	return !f.FreeOnly || sp.Free()
}

// NearbyQuery selects spots within RadiusM of (Lat, Lng) that match the filter,
// ordered by distance then ID. Offset/Limit page through the ordered result.
type NearbyQuery struct {
	SpotFilter
	Lat, Lng float64
	RadiusM  int
	Offset   int
	Limit    int
}

// BBoxQuery selects spots inside BBox that match the filter, ordered by distance
// from the bbox center then ID, at most Limit.
type BBoxQuery struct {
	SpotFilter
	domain.BBox
	Limit int
}

// SpotReader is what the HTTP API needs.
type SpotReader interface {
	NearbySpots(ctx context.Context, q NearbyQuery) ([]domain.Spot, error)
	SpotsInBBox(ctx context.Context, q BBoxQuery) ([]domain.Spot, error)
	// SpotByID returns ErrNotFound when the spot does not exist.
	// 끝난 행사(ends_at < now)도 반환한다 — 상세는 is_ended로 표시한다(TECH-PLAN.md §3).
	SpotByID(ctx context.Context, id string) (domain.Spot, error)
}

// SpotWriter is what the ingest pipeline needs.
// FetchedAt가 비어 있으면 구현체가 현재 시각으로 채운다.
type SpotWriter interface {
	UpsertSpot(ctx context.Context, s domain.Spot) error
}

// SpotStore is the full store.
type SpotStore interface {
	SpotReader
	SpotWriter
}
