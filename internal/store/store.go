// Package store defines persistence interfaces for spots.
// Implementations: store/memory (tests, local fake) and store/postgres (PostGIS + sqlc).
// store may import only internal/domain (see ARCHITECTURE.md).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// ErrNotFound is returned by SpotByID when no spot exists.
var ErrNotFound = errors.New("store: not found")

// NearbyQuery selects spots within RadiusM of (Lat, Lng) that overlap [From, To],
// ordered by distance then ID. Offset/Limit page through the ordered result.
//
// 구현체는 반드시 다음을 지킨다:
//   - ends_at < Now 인 항목은 반환하지 않는다 (끝난 행사 숨김).
//   - FreeOnly면 IsFree가 true인 항목만 (nil=모름은 제외).
type NearbyQuery struct {
	Lat, Lng   float64
	RadiusM    int
	Categories []domain.Category // empty = all
	FreeOnly   bool
	From, To   time.Time
	Now        time.Time
	Offset     int
	Limit      int
}

// SpotReader is what the HTTP API needs.
type SpotReader interface {
	NearbySpots(ctx context.Context, q NearbyQuery) ([]domain.Spot, error)
	// SpotByID returns ErrNotFound when the spot does not exist or has ended
	// (ends_at < now).
	SpotByID(ctx context.Context, id string, now time.Time) (domain.Spot, error)
}

// SpotWriter is what the ingest pipeline needs.
type SpotWriter interface {
	UpsertSpot(ctx context.Context, s domain.Spot) error
}

// SpotStore is the full store.
type SpotStore interface {
	SpotReader
	SpotWriter
}
