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
	saved map[string]map[string]time.Time // user ID → spot ID → saved_at
	now   func() time.Time
}

var (
	_ store.SpotStore  = (*Store)(nil)
	_ store.SavedStore = (*Store)(nil)
)

// New returns a store pre-filled with spots.
func New(spots ...domain.Spot) *Store {
	s := &Store{spots: make(map[string]domain.Spot), saved: make(map[string]map[string]time.Time), now: time.Now}
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

// PruneEnded implements store.SpotWriter.
func (s *Store) PruneEnded(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, sp := range s.spots {
		if sp.Ended(before) {
			s.deleteSpotLocked(id)
			n++
		}
	}
	return n, nil
}

// DeleteSpots implements store.SpotWriter.
func (s *Store) DeleteSpots(_ context.Context, ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range ids {
		if _, ok := s.spots[id]; ok {
			s.deleteSpotLocked(id)
			n++
		}
	}
	return n, nil
}

// deleteSpotLocked는 spot과 그 spot의 저장 행을 지운다(postgres의 ON DELETE CASCADE와 같음).
func (s *Store) deleteSpotLocked(id string) {
	delete(s.spots, id)
	for _, m := range s.saved {
		delete(m, id)
	}
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
		if !q.Match(sp) || !sp.MatchesKeyword(q.Keyword) {
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
