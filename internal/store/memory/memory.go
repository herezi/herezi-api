// Package memory is an in-memory store.SpotStore used by handler tests and
// for running the API without a database (DATABASE_URL unset).
package memory

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// Store is safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	spots map[string]domain.Spot
	now   func() time.Time
}

var _ store.SpotStore = (*Store)(nil)

// New returns a store pre-filled with spots.
func New(spots ...domain.Spot) *Store {
	s := &Store{spots: make(map[string]domain.Spot), now: time.Now}
	for _, sp := range spots {
		_ = s.UpsertSpot(context.Background(), sp)
	}
	return s
}

// UpsertSpot inserts or replaces by ID. A zero FetchedAt becomes now.
func (s *Store) UpsertSpot(_ context.Context, sp domain.Spot) error {
	if sp.FetchedAt.IsZero() {
		sp.FetchedAt = s.now()
	}
	sp.DistanceM = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spots[sp.ID] = sp
	return nil
}

// SpotByID returns store.ErrNotFound when missing. Ended spots are returned.
func (s *Store) SpotByID(_ context.Context, id string) (domain.Spot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sp, ok := s.spots[id]
	if !ok {
		return domain.Spot{}, store.ErrNotFound
	}
	return sp, nil
}

// distSpot pairs a spot with its distance for ordering.
type distSpot struct {
	sp domain.Spot
	d  float64
}

// sortByDistance orders by distance then ID, like the SQL ORDER BY.
func sortByDistance(xs []distSpot) {
	slices.SortFunc(xs, func(a, b distSpot) int {
		return cmp.Or(cmp.Compare(a.d, b.d), cmp.Compare(a.sp.ID, b.sp.ID))
	})
}

// NearbySpots mirrors the PostGIS query semantics (see store/postgres).
func (s *Store) NearbySpots(_ context.Context, q store.NearbyQuery) ([]domain.Spot, error) {
	s.mu.RLock()
	var hits []distSpot
	for _, sp := range s.spots {
		if !q.Match(sp) {
			continue
		}
		d := domain.DistanceM(q.Lat, q.Lng, sp.Lat, sp.Lng)
		if d > float64(q.RadiusM) {
			continue
		}
		hits = append(hits, distSpot{sp, d})
	}
	s.mu.RUnlock()
	sortByDistance(hits)
	if q.Offset >= len(hits) {
		return nil, nil
	}
	hits = hits[q.Offset:]
	if q.Limit > 0 && len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	out := make([]domain.Spot, 0, len(hits))
	for _, h := range hits {
		d := int(h.d + 0.5)
		h.sp.DistanceM = &d
		out = append(out, h.sp)
	}
	return out, nil
}

// SpotsInBBox mirrors the PostGIS query semantics (see store/postgres).
func (s *Store) SpotsInBBox(_ context.Context, q store.BBoxQuery) ([]domain.Spot, error) {
	cLat, cLng := q.Center()
	s.mu.RLock()
	var hits []distSpot
	for _, sp := range s.spots {
		if !q.Contains(sp.Lat, sp.Lng) || !q.Match(sp) {
			continue
		}
		hits = append(hits, distSpot{sp, domain.DistanceM(cLat, cLng, sp.Lat, sp.Lng)})
	}
	s.mu.RUnlock()
	sortByDistance(hits)
	if q.Limit > 0 && len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	out := make([]domain.Spot, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.sp)
	}
	return out, nil
}
