package memory

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// InsertNotice implements store.NoticeStore.
func (s *Store) InsertNotice(_ context.Context, n domain.Notice) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastNoticeID++
	n.ID = s.lastNoticeID
	s.notices = append(s.notices, n)
	return n.ID, nil
}

// Notices implements store.NoticeReader.
func (s *Store) Notices(_ context.Context, now time.Time, limit int) ([]domain.Notice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Notice{}
	for _, n := range s.notices {
		if n.Published(now) {
			n.Body = ""
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, b domain.Notice) int {
		if c := b.PublishedAt.Compare(a.PublishedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// NoticeByID implements store.NoticeReader.
func (s *Store) NoticeByID(_ context.Context, id int64, now time.Time) (domain.Notice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.notices {
		if n.ID == id && n.Published(now) {
			return n, nil
		}
	}
	return domain.Notice{}, store.ErrNotFound
}
