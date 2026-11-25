package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// NoticeFixtures는 공지 스위트가 넣는 공지다. 제목이 기대 결과를 말한다
// ("hidden"·"scheduled"는 목록·상세 모두에서 보이지 않는다).
func NoticeFixtures() []domain.Notice {
	h := time.Hour
	n := func(title string, at time.Time, hidden bool) domain.Notice {
		return domain.Notice{Title: title, Body: "# " + title + "\n\n본문", PublishedAt: at, IsHidden: hidden}
	}
	return []domain.Notice{
		n("old", Now.Add(-48*h), false),
		n("newest", Now.Add(-h), false),
		n("hidden", Now.Add(-2*h), true),
		n("scheduled", Now.Add(h), false),
		n("exactly-now", Now, false),
		n("same-time-first", Now.Add(-24*h), false),
		n("same-time-second", Now.Add(-24*h), false), // 같은 시각이면 ID 큰 것(나중에 넣은 것)이 먼저
	}
}

// RunNotices는 공지 저장소 동작 스위트다. newStore는 공지가 없는 저장소를 돌려줘야 한다.
func RunNotices(t *testing.T, newStore func(t *testing.T) store.NoticeStore) {
	ctx := context.Background()
	setup := func(t *testing.T) (store.NoticeStore, map[string]int64) {
		t.Helper()
		s := newStore(t)
		ids := map[string]int64{}
		for _, n := range NoticeFixtures() {
			id, err := s.InsertNotice(ctx, n)
			if err != nil {
				t.Fatalf("insert %s: %v", n.Title, err)
			}
			ids[n.Title] = id
		}
		return s, ids
	}
	titles := func(ns []domain.Notice) []string {
		var out []string
		for _, n := range ns {
			out = append(out, n.Title)
		}
		return out
	}

	t.Run("list published newest first without body", func(t *testing.T) {
		s, ids := setup(t)
		got, err := s.Notices(ctx, Now, store.MaxNotices)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"exactly-now", "newest", "same-time-second", "same-time-first", "old"}
		if !slices.Equal(titles(got), want) {
			t.Fatalf("titles = %v, want %v", titles(got), want)
		}
		for _, n := range got {
			if n.Body != "" {
				t.Errorf("%s: list body = %q, want empty", n.Title, n.Body)
			}
			if n.ID != ids[n.Title] {
				t.Errorf("%s: id = %d, want %d", n.Title, n.ID, ids[n.Title])
			}
			if !n.PublishedAt.Equal(publishedAt(n.Title)) {
				t.Errorf("%s: published_at = %v", n.Title, n.PublishedAt)
			}
		}
	})

	t.Run("limit", func(t *testing.T) {
		s, _ := setup(t)
		got, err := s.Notices(ctx, Now, 2)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"exactly-now", "newest"}; !slices.Equal(titles(got), want) {
			t.Fatalf("titles = %v, want %v", titles(got), want)
		}
	})

	t.Run("scheduled appears after published_at", func(t *testing.T) {
		s, _ := setup(t)
		got, err := s.Notices(ctx, Now.Add(2*time.Hour), store.MaxNotices)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 || got[0].Title != "scheduled" {
			t.Fatalf("titles = %v, want scheduled first", titles(got))
		}
	})

	t.Run("by id", func(t *testing.T) {
		s, ids := setup(t)
		n, err := s.NoticeByID(ctx, ids["newest"], Now)
		if err != nil {
			t.Fatal(err)
		}
		if n.Title != "newest" || n.Body != "# newest\n\n본문" || n.ID != ids["newest"] {
			t.Fatalf("notice = %+v", n)
		}
	})

	t.Run("by id hidden, scheduled, unknown are not found", func(t *testing.T) {
		s, ids := setup(t)
		for _, id := range []int64{ids["hidden"], ids["scheduled"], 999999} {
			if _, err := s.NoticeByID(ctx, id, Now); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("id %d: err = %v, want ErrNotFound", id, err)
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		got, err := newStore(t).Notices(ctx, Now, store.MaxNotices)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("notices = %#v, want empty non-nil", got)
		}
	})
}

func publishedAt(title string) time.Time {
	for _, n := range NoticeFixtures() {
		if n.Title == title {
			return n.PublishedAt
		}
	}
	return time.Time{}
}
