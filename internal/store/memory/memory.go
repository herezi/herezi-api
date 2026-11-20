// Package memory is an in-memory store.SpotStore used by handler tests and
// for running the API without a database (DATABASE_URL unset).
package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// Store is safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	spots map[string]domain.Spot
}

var _ store.SpotStore = (*Store)(nil)

// New returns a store pre-filled with spots.
func New(spots ...domain.Spot) *Store {
	s := &Store{spots: make(map[string]domain.Spot)}
	for _, sp := range spots {
		s.spots[sp.ID] = sp
	}
	return s
}

// UpsertSpot inserts or replaces by ID.
func (s *Store) UpsertSpot(_ context.Context, sp domain.Spot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spots[sp.ID] = sp
	return nil
}

// SpotByID returns store.ErrNotFound when missing or ended.
func (s *Store) SpotByID(_ context.Context, id string, now time.Time) (domain.Spot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sp, ok := s.spots[id]
	if !ok || sp.Ended(now) {
		return domain.Spot{}, store.ErrNotFound
	}
	return sp, nil
}

// NearbySpots mirrors the PostGIS query semantics (see store/postgres/queries.sql).
func (s *Store) NearbySpots(_ context.Context, q store.NearbyQuery) ([]domain.Spot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.Spot
	for _, sp := range s.spots {
		if sp.Ended(q.Now) {
			continue
		}
		if len(q.Categories) > 0 && !slices.Contains(q.Categories, sp.Category) {
			continue
		}
		if q.FreeOnly && !sp.Free() {
			continue
		}
		if !sp.Overlaps(q.From, q.To) {
			continue
		}
		d := int(domain.DistanceM(q.Lat, q.Lng, sp.Lat, sp.Lng))
		if d > q.RadiusM {
			continue
		}
		sp.DistanceM = &d
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool {
		if *out[i].DistanceM != *out[j].DistanceM {
			return *out[i].DistanceM < *out[j].DistanceM
		}
		return out[i].ID < out[j].ID
	})
	if q.Offset >= len(out) {
		return nil, nil
	}
	out = out[q.Offset:]
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}
