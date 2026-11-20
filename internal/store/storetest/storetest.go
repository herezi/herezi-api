// Package storetest is a behavior suite every store.SpotStore must pass.
// memory는 일반 테스트로, postgres는 통합 테스트(-tags integration)로 같은 스위트를 돌려
// 두 구현의 조회 규칙이 어긋나지 않게 한다.
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

// Now is the fixed clock used by the suite (Wednesday 15:00 KST).
var Now = time.Date(2026, 11, 18, 15, 0, 0, 0, domain.Seoul)

// FetchedAt is set on every fixture.
var FetchedAt = Now.Add(-30 * time.Minute)

// City Hall, Seoul.
const baseLat, baseLng = 37.5663, 126.9779

// Fixtures returns the spots the suite inserts. IDs encode the expectation.
func Fixtures() []domain.Spot {
	h := time.Hour
	ptr := func(t time.Time) *time.Time { return &t }
	yes, no := true, false
	sp := func(id string, c domain.Category, dLat float64, start time.Time, end *time.Time, free *bool) domain.Spot {
		return domain.Spot{
			ID: domain.SpotID("test", id), Source: "test", SourceID: id, Title: id,
			Category: c, Lat: baseLat + dLat, Lng: baseLng, StartsAt: start, EndsAt: end, IsFree: free,
			FetchedAt: FetchedAt,
		}
	}
	return []domain.Spot{
		// ~0m, ongoing, free
		sp("near-festival-free", domain.CategoryFestival, 0, Now.Add(-h), ptr(Now.Add(h)), &yes),
		// ~1.1km, ongoing, not free
		sp("mid-popup", domain.CategoryPopup, 0.01, Now.Add(-h), ptr(Now.Add(48*h)), &no),
		// ~2.2km, ongoing, free unknown
		sp("far-exhibition-unknown", domain.CategoryExhibition, 0.02, Now.Add(-h), ptr(Now.Add(48*h)), nil),
		// ~0.5km, ended an hour ago
		sp("ended", domain.CategoryFestival, 0.005, Now.Add(-3*h), ptr(Now.Add(-h)), &yes),
		// ~0.6km, starts tomorrow
		sp("tomorrow", domain.CategoryPerformance, 0.006, Now.Add(24*h), ptr(Now.Add(26*h)), &yes),
		// ~11km, ongoing
		sp("too-far", domain.CategoryFestival, 0.1, Now.Add(-h), ptr(Now.Add(h)), &yes),
	}
}

// Run executes the suite. newStore must return an empty store.
func Run(t *testing.T, newStore func(t *testing.T) store.SpotStore) {
	ctx := context.Background()
	setup := func(t *testing.T) store.SpotStore {
		t.Helper()
		s := newStore(t)
		for _, sp := range Fixtures() {
			if err := s.UpsertSpot(ctx, sp); err != nil {
				t.Fatalf("upsert %s: %v", sp.ID, err)
			}
		}
		return s
	}
	ids := func(spots []domain.Spot) []string {
		out := make([]string, 0, len(spots))
		for _, sp := range spots {
			out = append(out, sp.SourceID)
		}
		return out
	}
	base := func() store.NearbyQuery {
		from, to := domain.WhenNow.Window(Now)
		return store.NearbyQuery{
			SpotFilter: store.SpotFilter{From: from, To: to, Now: Now},
			Lat:        baseLat, Lng: baseLng, RadiusM: 3000, Limit: 100,
		}
	}

	cases := []struct {
		name string
		mod  func(q *store.NearbyQuery)
		want []string
	}{
		{"now ordered by distance, ended and future hidden", func(*store.NearbyQuery) {},
			[]string{"near-festival-free", "mid-popup", "far-exhibition-unknown"}},
		{"radius", func(q *store.NearbyQuery) { q.RadiusM = 1500 },
			[]string{"near-festival-free", "mid-popup"}},
		{"categories", func(q *store.NearbyQuery) {
			q.Categories = []domain.Category{domain.CategoryPopup, domain.CategoryExhibition}
		}, []string{"mid-popup", "far-exhibition-unknown"}},
		{"free only excludes false and unknown", func(q *store.NearbyQuery) { q.FreeOnly = true },
			[]string{"near-festival-free"}},
		{"week includes future, still hides ended", func(q *store.NearbyQuery) {
			q.From, q.To = domain.WhenWeek.Window(Now)
		}, []string{"near-festival-free", "tomorrow", "mid-popup", "far-exhibition-unknown"}},
		{"ended hidden even if window reaches into the past", func(q *store.NearbyQuery) {
			q.From = Now.Add(-10 * time.Hour)
		}, []string{"near-festival-free", "mid-popup", "far-exhibition-unknown"}},
		{"offset and limit", func(q *store.NearbyQuery) { q.Offset, q.Limit = 1, 1 },
			[]string{"mid-popup"}},
	}
	s := setup(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := base()
			tc.mod(&q)
			got, err := s.NearbySpots(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("got %v, want %v", ids(got), tc.want)
			}
			for _, sp := range got {
				if sp.DistanceM == nil {
					t.Fatalf("%s: DistanceM not set", sp.ID)
				}
			}
		})
	}

	// bbox: 시청 바로 남쪽 ~ 북쪽 1.6km, 경도 ±0.001. 중심(lat+0.007)에서 가까운 순.
	bbox := func() store.BBoxQuery {
		from, to := domain.WhenNow.Window(Now)
		return store.BBoxQuery{
			SpotFilter: store.SpotFilter{From: from, To: to, Now: Now},
			BBox:       domain.BBox{SwLat: baseLat - 0.001, SwLng: baseLng - 0.001, NeLat: baseLat + 0.015, NeLng: baseLng + 0.001},
			Limit:      100,
		}
	}
	bboxCases := []struct {
		name string
		mod  func(q *store.BBoxQuery)
		want []string
	}{
		{"bbox now ordered from center, ended and future hidden", func(*store.BBoxQuery) {},
			[]string{"mid-popup", "near-festival-free"}},
		{"bbox limit", func(q *store.BBoxQuery) { q.Limit = 1 }, []string{"mid-popup"}},
		{"bbox free only", func(q *store.BBoxQuery) { q.FreeOnly = true }, []string{"near-festival-free"}},
		{"bbox category", func(q *store.BBoxQuery) { q.Categories = []domain.Category{domain.CategoryFestival} },
			[]string{"near-festival-free"}},
		{"bbox week", func(q *store.BBoxQuery) { q.From, q.To = domain.WhenWeek.Window(Now) },
			[]string{"tomorrow", "mid-popup", "near-festival-free"}},
		{"bbox lng excludes", func(q *store.BBoxQuery) { q.SwLng, q.NeLng = baseLng+0.0005, baseLng+0.01 },
			[]string{}},
		{"bbox large includes far", func(q *store.BBoxQuery) { q.NeLat = baseLat + 0.2 },
			[]string{"too-far", "far-exhibition-unknown", "mid-popup", "near-festival-free"}},
	}
	for _, tc := range bboxCases {
		t.Run(tc.name, func(t *testing.T) {
			q := bbox()
			tc.mod(&q)
			got, err := s.SpotsInBBox(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("got %v, want %v", ids(got), tc.want)
			}
			for _, sp := range got {
				if sp.DistanceM != nil {
					t.Fatalf("%s: DistanceM must be nil for bbox", sp.ID)
				}
			}
		})
	}

	t.Run("distance roughly right", func(t *testing.T) {
		q := base()
		q.Categories = []domain.Category{domain.CategoryPopup}
		got, err := s.NearbySpots(ctx, q)
		if err != nil || len(got) != 1 {
			t.Fatalf("got %v, %v", got, err)
		}
		if d := *got[0].DistanceM; d < 1050 || d > 1180 {
			t.Fatalf("distance = %d, want ~1112", d)
		}
	})

	t.Run("by id", func(t *testing.T) {
		got, err := s.SpotByID(ctx, domain.SpotID("test", "mid-popup"), Now)
		if err != nil {
			t.Fatal(err)
		}
		want := Fixtures()[1]
		if got.Title != want.Title || got.Category != want.Category || got.IsFree == nil || *got.IsFree ||
			got.EndsAt == nil || !got.EndsAt.Equal(*want.EndsAt) || !got.StartsAt.Equal(want.StartsAt) ||
			!got.FetchedAt.Equal(FetchedAt) {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("by id hides ended and missing", func(t *testing.T) {
		for _, id := range []string{domain.SpotID("test", "ended"), "test:nope"} {
			if _, err := s.SpotByID(ctx, id, Now); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("%s: err = %v, want ErrNotFound", id, err)
			}
		}
	})

	t.Run("upsert without fetched_at sets it", func(t *testing.T) {
		sp := Fixtures()[2]
		sp.FetchedAt = time.Time{}
		before := time.Now().Add(-time.Second)
		if err := s.UpsertSpot(ctx, sp); err != nil {
			t.Fatal(err)
		}
		got, err := s.SpotByID(ctx, sp.ID, Now)
		if err != nil || got.FetchedAt.Before(before) {
			t.Fatalf("fetched_at = %v, %v", got.FetchedAt, err)
		}
	})

	t.Run("upsert replaces", func(t *testing.T) {
		sp := Fixtures()[1]
		sp.Title = "changed"
		if err := s.UpsertSpot(ctx, sp); err != nil {
			t.Fatal(err)
		}
		got, err := s.SpotByID(ctx, sp.ID, Now)
		if err != nil || got.Title != "changed" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}
