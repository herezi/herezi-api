package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// SavedSpotStore는 저장 목록 스위트가 쓰는 저장소(스팟 + 저장).
type SavedSpotStore interface {
	store.SpotStore
	store.SavedStore
}

// 스위트가 쓰는 사용자 ID(Supabase sub 형식).
const (
	UserA = "11111111-1111-4111-8111-111111111111"
	UserB = "22222222-2222-4222-8222-222222222222"
)

// RunSaved는 저장 목록 동작 스위트다. newStore는 빈 저장소를 돌려줘야 한다.
func RunSaved(t *testing.T, newStore func(t *testing.T) SavedSpotStore) {
	ctx := context.Background()
	setup := func(t *testing.T) SavedSpotStore {
		t.Helper()
		s := newStore(t)
		for _, sp := range Fixtures() {
			if err := s.UpsertSpot(ctx, sp); err != nil {
				t.Fatalf("upsert %s: %v", sp.ID, err)
			}
		}
		return s
	}
	id := func(name string) string { return domain.SpotID("test", name) }
	list := func(t *testing.T, s SavedSpotStore, q store.SavedQuery) []string {
		t.Helper()
		got, err := s.SavedSpots(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, g := range got {
			out = append(out, g.Spot.SourceID)
		}
		return out
	}
	save := func(t *testing.T, s SavedSpotStore, user, name string, at time.Time) {
		t.Helper()
		if err := s.SaveSpot(ctx, user, id(name), at); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	t.Run("save list unsave", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "mid-popup", Now.Add(-3*time.Hour))
		save(t, s, UserA, "ended", Now.Add(-2*time.Hour))
		save(t, s, UserA, "tomorrow", Now.Add(-time.Hour))
		save(t, s, UserB, "too-far", Now)

		if got, want := list(t, s, store.SavedQuery{UserID: UserA}), []string{"tomorrow", "ended", "mid-popup"}; !slices.Equal(got, want) {
			t.Fatalf("newest first: got %v, want %v", got, want)
		}
		got, err := s.SavedSpots(ctx, store.SavedQuery{UserID: UserA, Limit: 1})
		if err != nil || len(got) != 1 {
			t.Fatalf("got %v, %v", got, err)
		}
		if !got[0].SavedAt.Equal(Now.Add(-time.Hour)) || got[0].Spot.Title != "tomorrow" || !got[0].Spot.FetchedAt.Equal(FetchedAt) {
			t.Fatalf("snapshot = %+v", got[0])
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA, Offset: 1, Limit: 1}); !slices.Equal(got, []string{"ended"}) {
			t.Fatalf("offset: %v", got)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA, Offset: 10}); len(got) != 0 {
			t.Fatalf("offset past end: %v", got)
		}
		// 끝난 항목도 목록에 나온다(is_ended는 핸들러가 계산). 보존 기간이 지난 것만 숨긴다.
		ended := Fixtures()[3]
		if got := list(t, s, store.SavedQuery{UserID: UserA, HideEndedBefore: ended.EndsAt.Add(time.Minute)}); !slices.Equal(got, []string{"tomorrow", "mid-popup"}) {
			t.Fatalf("hide ended before: %v", got)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserB}); !slices.Equal(got, []string{"too-far"}) {
			t.Fatalf("user B: %v", got)
		}

		if err := s.UnsaveSpot(ctx, UserA, id("ended")); err != nil {
			t.Fatal(err)
		}
		if err := s.UnsaveSpot(ctx, UserA, id("ended")); err != nil { // 멱등
			t.Fatal(err)
		}
		if err := s.UnsaveSpot(ctx, UserA, "test:nope"); err != nil {
			t.Fatal(err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); !slices.Equal(got, []string{"tomorrow", "mid-popup"}) {
			t.Fatalf("after unsave: %v", got)
		}
	})

	t.Run("save is idempotent and keeps saved_at", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "mid-popup", Now.Add(-time.Hour))
		save(t, s, UserA, "mid-popup", Now)
		got, err := s.SavedSpots(ctx, store.SavedQuery{UserID: UserA})
		if err != nil || len(got) != 1 || !got[0].SavedAt.Equal(Now.Add(-time.Hour)) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("save unknown spot", func(t *testing.T) {
		s := setup(t)
		if err := s.SaveSpot(ctx, UserA, "test:nope", Now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("pruned and deleted spots disappear from saved", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "ended", Now)
		save(t, s, UserA, "mid-popup", Now.Add(-time.Minute))
		save(t, s, UserB, "ended", Now)
		if _, err := s.PruneEnded(ctx, Now); err != nil {
			t.Fatal(err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); !slices.Equal(got, []string{"mid-popup"}) {
			t.Fatalf("after prune: %v", got)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserB}); len(got) != 0 {
			t.Fatalf("after prune user B: %v", got)
		}
		if _, err := s.DeleteSpots(ctx, []string{id("mid-popup")}); err != nil {
			t.Fatal(err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); len(got) != 0 {
			t.Fatalf("after delete: %v", got)
		}
		// 다시 수집돼도 저장은 되살아나지 않는다.
		if err := s.UpsertSpot(ctx, Fixtures()[1]); err != nil {
			t.Fatal(err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); len(got) != 0 {
			t.Fatalf("after re-upsert: %v", got)
		}
	})

	t.Run("upsert keeps saved rows", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "mid-popup", Now)
		sp := Fixtures()[1]
		sp.Title = "changed"
		if err := s.UpsertSpot(ctx, sp); err != nil {
			t.Fatal(err)
		}
		got, err := s.SavedSpots(ctx, store.SavedQuery{UserID: UserA})
		if err != nil || len(got) != 1 || got[0].Spot.Title != "changed" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("import", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "mid-popup", Now.Add(-time.Hour))
		res, err := s.ImportSaved(ctx, UserA,
			[]string{id("mid-popup"), id("ended"), id("tomorrow"), id("tomorrow"), "test:gone", "test:gone2"}, Now)
		if err != nil {
			t.Fatal(err)
		}
		if res != (store.ImportResult{Imported: 2, SkippedUnknown: 2}) {
			t.Fatalf("result = %+v", res)
		}
		got, err := s.SavedSpots(ctx, store.SavedQuery{UserID: UserA})
		if err != nil || len(got) != 3 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got[2].Spot.SourceID != "mid-popup" || !got[2].SavedAt.Equal(Now.Add(-time.Hour)) {
			t.Fatalf("existing saved_at must be kept: %+v", got[2])
		}
		res, err = s.ImportSaved(ctx, UserA, nil, Now)
		if err != nil || res != (store.ImportResult{}) {
			t.Fatalf("empty import: %+v, %v", res, err)
		}
	})

	t.Run("limit", func(t *testing.T) {
		s := setup(t)
		base := Fixtures()[0]
		var bulk []string
		for i := range store.MaxSavedPerUser + 1 {
			sp := base
			sp.SourceID = fmt.Sprintf("bulk-%03d", i)
			sp.ID = id(sp.SourceID)
			if err := s.UpsertSpot(ctx, sp); err != nil {
				t.Fatal(err)
			}
			bulk = append(bulk, sp.ID)
		}
		if _, err := s.ImportSaved(ctx, UserA, bulk, Now); !errors.Is(err, store.ErrSavedLimit) {
			t.Fatalf("import over limit: err = %v", err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); len(got) != 0 {
			t.Fatalf("over-limit import must save nothing, got %d", len(got))
		}
		res, err := s.ImportSaved(ctx, UserA, bulk[:store.MaxSavedPerUser], Now)
		if err != nil || res.Imported != store.MaxSavedPerUser {
			t.Fatalf("import up to limit: %+v, %v", res, err)
		}
		if err := s.SaveSpot(ctx, UserA, bulk[store.MaxSavedPerUser], Now); !errors.Is(err, store.ErrSavedLimit) {
			t.Fatalf("save over limit: err = %v", err)
		}
		if err := s.SaveSpot(ctx, UserA, bulk[0], Now); err != nil { // 이미 저장된 건 한도와 무관
			t.Fatalf("re-save at limit: %v", err)
		}
		if err := s.SaveSpot(ctx, UserA, "test:nope", Now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown at limit: err = %v, want ErrNotFound", err)
		}
		if err := s.SaveSpot(ctx, UserB, bulk[0], Now); err != nil { // 다른 사용자는 별개
			t.Fatal(err)
		}
	})

	t.Run("delete user saved", func(t *testing.T) {
		s := setup(t)
		save(t, s, UserA, "mid-popup", Now)
		save(t, s, UserA, "ended", Now)
		save(t, s, UserB, "mid-popup", Now)
		n, err := s.DeleteUserSaved(ctx, UserA)
		if err != nil || n != 2 {
			t.Fatalf("deleted %d, %v", n, err)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserA}); len(got) != 0 {
			t.Fatalf("user A: %v", got)
		}
		if got := list(t, s, store.SavedQuery{UserID: UserB}); !slices.Equal(got, []string{"mid-popup"}) {
			t.Fatalf("user B must be untouched: %v", got)
		}
		if n, err := s.DeleteUserSaved(ctx, UserA); err != nil || n != 0 {
			t.Fatalf("second delete %d, %v", n, err)
		}
	})
}
