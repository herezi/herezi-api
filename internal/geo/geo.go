// Package geo는 좌표 → 행정구역 이름(역지오코딩)을 다룬다.
// 외부 서비스(네이버 Reverse Geocoding)는 ReverseGeocoder 인터페이스 뒤에 숨겨
// 핸들러는 가짜 구현으로 테스트한다. domain 외의 internal 패키지를 import하지 않는다.
package geo

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
)

// Region은 행정구역 이름. 결과가 없으면(바다 등) 모든 필드가 빈 문자열이다.
type Region struct {
	Area1 string // 시·도 (예: 서울특별시)
	Area2 string // 시·군·구 (예: 중구)
	Area3 string // 읍·면·동 (예: 태평로1가)
}

// Label은 앱 상단에 보여줄 짧은 위치명. "중구 태평로1가"처럼 area2 + area3이고,
// 둘 다 없으면 area1, 그것도 없으면 빈 문자열이다.
func (r Region) Label() string {
	var parts []string
	for _, p := range []string{r.Area2, r.Area3} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(r.Area1)
	}
	return strings.Join(parts, " ")
}

// ReverseGeocoder는 좌표를 행정구역으로 바꾼다.
type ReverseGeocoder interface {
	Reverse(ctx context.Context, lat, lng float64) (Region, error)
}

// RoundCoord는 좌표를 소수 3자리(약 100m)로 반올림한다. 캐시 키와 외부 요청에 쓴다.
func RoundCoord(v float64) float64 { return math.Round(v*1000) / 1000 }

// Cache는 ReverseGeocoder 결과를 반올림 좌표 기준으로 메모리에 TTL 동안 보관한다.
// 실패는 캐시하지 않는다.
type Cache struct {
	next ReverseGeocoder
	ttl  time.Duration
	max  int
	now  func() time.Time

	mu      sync.Mutex
	entries map[[2]float64]cacheEntry
}

type cacheEntry struct {
	region  Region
	expires time.Time
}

// DefaultCacheTTL은 역지오코딩 캐시 유지 시간(1일).
const DefaultCacheTTL = 24 * time.Hour

const defaultCacheMax = 50_000

// NewCache는 next 앞에 메모리 캐시를 둔다. ttl <= 0이면 DefaultCacheTTL.
func NewCache(next ReverseGeocoder, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{next: next, ttl: ttl, max: defaultCacheMax, now: time.Now, entries: map[[2]float64]cacheEntry{}}
}

// Reverse는 좌표를 반올림한 뒤 캐시를 먼저 보고, 없으면 next를 부른다.
func (c *Cache) Reverse(ctx context.Context, lat, lng float64) (Region, error) {
	lat, lng = RoundCoord(lat), RoundCoord(lng)
	key := [2]float64{lat, lng}
	now := c.now()

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.region, nil
	}
	c.mu.Unlock()

	r, err := c.next.Reverse(ctx, lat, lng)
	if err != nil {
		return Region{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.max {
			clear(c.entries)
		}
	}
	c.entries[key] = cacheEntry{region: r, expires: now.Add(c.ttl)}
	return r, nil
}
