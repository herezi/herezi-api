package memory

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/herezi/herezi-api/internal/store"
)

// SaveSpot implements store.SavedStore.
func (s *Store) SaveSpot(_ context.Context, userID, spotID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spots[spotID]; !ok {
		return store.ErrNotFound
	}
	m := s.saved[userID]
	if _, ok := m[spotID]; ok {
		return nil
	}
	if len(m) >= store.MaxSavedPerUser {
		return store.ErrSavedLimit
	}
	if m == nil {
		m = make(map[string]time.Time)
		s.saved[userID] = m
	}
	m[spotID] = at
	return nil
}

// UnsaveSpot implements store.SavedStore.
func (s *Store) UnsaveSpot(_ context.Context, userID, spotID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.saved[userID], spotID)
	return nil
}

// SavedSpots implements store.SavedStore.
func (s *Store) SavedSpots(_ context.Context, q store.SavedQuery) ([]store.SavedSpot, error) {
	s.mu.RLock()
	var out []store.SavedSpot
	for id, at := range s.saved[q.UserID] {
		sp, ok := s.spots[id]
		if !ok || (!q.HideEndedBefore.IsZero() && sp.Ended(q.HideEndedBefore)) {
			continue
		}
		out = append(out, store.SavedSpot{Spot: sp, SavedAt: at})
	}
	s.mu.RUnlock()
	slices.SortFunc(out, func(a, b store.SavedSpot) int {
		return cmp.Or(b.SavedAt.Compare(a.SavedAt), cmp.Compare(a.Spot.ID, b.Spot.ID))
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

// ImportSaved implements store.SavedStore.
func (s *Store) ImportSaved(_ context.Context, userID string, spotIDs []string, at time.Time) (store.ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res store.ImportResult
	m := s.saved[userID]
	var fresh []string
	seen := make(map[string]bool, len(spotIDs))
	for _, id := range spotIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := s.spots[id]; !ok {
			res.SkippedUnknown++
			continue
		}
		if _, ok := m[id]; !ok {
			fresh = append(fresh, id)
		}
	}
	if len(m)+len(fresh) > store.MaxSavedPerUser {
		return store.ImportResult{}, store.ErrSavedLimit
	}
	if m == nil {
		m = make(map[string]time.Time)
		s.saved[userID] = m
	}
	for _, id := range fresh {
		m[id] = at
	}
	res.Imported = len(fresh)
	return res, nil
}

// DeleteUserSaved implements store.SavedStore.
func (s *Store) DeleteUserSaved(_ context.Context, userID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.saved[userID])
	delete(s.saved, userID)
	return n, nil
}
