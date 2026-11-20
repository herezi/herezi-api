package geo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRegionLabel(t *testing.T) {
	for _, tc := range []struct {
		r    Region
		want string
	}{
		{Region{"서울특별시", "중구", "태평로1가"}, "중구 태평로1가"},
		{Region{"경기도", "성남시 분당구", "정자동"}, "성남시 분당구 정자동"},
		{Region{"세종특별자치시", "", "조치원읍"}, "조치원읍"},
		{Region{"제주특별자치도", "", ""}, "제주특별자치도"},
		{Region{}, ""},
	} {
		if got := tc.r.Label(); got != tc.want {
			t.Errorf("%+v.Label() = %q, want %q", tc.r, got, tc.want)
		}
	}
}

type countingGeocoder struct {
	calls int
	last  [2]float64
	err   error
}

func (c *countingGeocoder) Reverse(_ context.Context, lat, lng float64) (Region, error) {
	c.calls++
	c.last = [2]float64{lat, lng}
	if c.err != nil {
		return Region{}, c.err
	}
	return Region{"서울특별시", "중구", "태평로1가"}, nil
}

func TestCacheRoundsAndExpires(t *testing.T) {
	next := &countingGeocoder{}
	c := NewCache(next, time.Hour)
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := c.Reverse(ctx, 37.56631, 126.97791); err != nil {
		t.Fatal(err)
	}
	if next.last != [2]float64{37.566, 126.978} {
		t.Fatalf("next called with %v, want rounded coords", next.last)
	}
	// 반올림하면 같은 칸 → 캐시 적중
	r, err := c.Reverse(ctx, 37.56649, 126.97751)
	if err != nil || r.Label() != "중구 태평로1가" || next.calls != 1 {
		t.Fatalf("cache miss: calls=%d r=%+v err=%v", next.calls, r, err)
	}
	now = now.Add(time.Hour)
	if _, err := c.Reverse(ctx, 37.566, 126.978); err != nil || next.calls != 2 {
		t.Fatalf("expired entry not refreshed: calls=%d err=%v", next.calls, err)
	}
}

func TestCacheDoesNotStoreErrors(t *testing.T) {
	next := &countingGeocoder{err: errors.New("boom")}
	c := NewCache(next, 0)
	ctx := context.Background()
	for range 2 {
		if _, err := c.Reverse(ctx, 37.5, 127.0); err == nil {
			t.Fatal("want error")
		}
	}
	if next.calls != 2 {
		t.Fatalf("calls = %d, want 2 (errors must not be cached)", next.calls)
	}
}
