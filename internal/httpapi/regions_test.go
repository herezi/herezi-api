package httpapi_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

// regionFixtures: 서울 시청 근처 항목들. ID가 기대 결과를 말한다.
func regionFixtures(now time.Time) []domain.Spot {
	day := 24 * time.Hour
	ptr := func(t time.Time) *time.Time { return &t }
	sp := func(id string, lat, lng float64, start time.Time, end *time.Time) domain.Spot {
		return domain.Spot{
			ID: domain.SpotID("test", id), Source: "test", SourceID: id, Title: id,
			Category: domain.CategoryFestival, Lat: lat, Lng: lng, StartsAt: start, EndsAt: end,
			FetchedAt: now.Add(-time.Hour),
		}
	}
	const lat, lng = 37.5663, 126.9779
	return []domain.Spot{
		sp("in-ongoing", lat, lng, now.Add(-day), ptr(now.Add(day))),
		sp("in-ended-89d", lat, lng, now.Add(-100*day), ptr(now.Add(-89*day))),
		sp("out-ended-91d", lat, lng, now.Add(-100*day), ptr(now.Add(-91*day))),
		sp("in-starts-89d", lat, lng, now.Add(89*day), ptr(now.Add(95*day))),
		sp("out-starts-91d", lat, lng, now.Add(91*day), ptr(now.Add(95*day))),
		sp("in-no-end-recent", lat, lng, now.Add(-10*day), nil),
		sp("out-no-end-old", lat, lng, now.Add(-91*day), nil),
		sp("out-busan", 35.1796, 129.0756, now.Add(-day), ptr(now.Add(day))),
	}
}

type regionBody struct {
	Region      string    `json:"region"`
	GeneratedAt time.Time `json:"generated_at"`
	Count       int       `json:"count"`
	Items       []struct {
		ID        string          `json:"id"`
		IsEnded   bool            `json:"is_ended"`
		DistanceM json.RawMessage `json:"distance_m"`
		SavedAt   json.RawMessage `json:"saved_at"`
	} `json:"items"`
}

type regionEnv struct {
	srv   *httptest.Server
	store *memory.Store
	mu    sync.Mutex
	now   time.Time
}

func (e *regionEnv) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *regionEnv) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

func newRegionEnv(t *testing.T, reader func(*memory.Store) store.SpotReader) *regionEnv {
	t.Helper()
	e := &regionEnv{now: storetest.Now, store: memory.New(regionFixtures(storetest.Now)...)}
	var r store.SpotReader = e.store
	if reader != nil {
		r = reader(e.store)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.srv = httptest.NewServer(httpapi.New(r, log, httpapi.WithClock(e.clock)).Handler())
	t.Cleanup(e.srv.Close)
	return e
}

// fetchRegion sends a raw request (Transport가 gzip을 자동으로 풀지 않게 Accept-Encoding을 직접 넣는다).
func fetchRegion(t *testing.T, e *regionEnv, path string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "identity")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, b
}

func decodeRegion(t *testing.T, b []byte) regionBody {
	t.Helper()
	var body regionBody
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("decode: %v (%q)", err, b)
	}
	return body
}

func regionIDs(b regionBody) []string {
	var ids []string
	for _, it := range b.Items {
		ids = append(ids, strings.TrimPrefix(it.ID, "test:"))
	}
	return ids
}

const seoulPath = "/v1/regions/seoul/spots"

func TestRegionSpotsContent(t *testing.T) {
	e := newRegionEnv(t, nil)
	res, b := fetchRegion(t, e, seoulPath, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, b)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type %q", ct)
	}
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("identity request got Content-Encoding %q", res.Header.Get("Content-Encoding"))
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("Cache-Control %q", got)
	}
	if got := res.Header.Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary %q", got)
	}
	if et := res.Header.Get("ETag"); !strings.HasPrefix(et, `"`) || strings.HasPrefix(et, "W/") {
		t.Fatalf("ETag %q must be a strong validator", et)
	}
	body := decodeRegion(t, b)
	// 종료 후 90일 안은 포함, 넘으면 제외 / 90일 안에 시작은 포함, 넘으면 제외 / 지역 밖 제외. ID순.
	want := []string{"in-ended-89d", "in-no-end-recent", "in-ongoing", "in-starts-89d"}
	if got := regionIDs(body); !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v", got, want)
	}
	if body.Region != "seoul" || body.Count != len(want) || !body.GeneratedAt.Equal(storetest.Now) {
		t.Fatalf("region=%q count=%d generated_at=%v", body.Region, body.Count, body.GeneratedAt)
	}
	for _, it := range body.Items {
		if it.DistanceM != nil || it.SavedAt != nil {
			t.Fatalf("%s: distance_m/saved_at must be absent", it.ID)
		}
		if wantEnded := it.ID == "test:in-ended-89d"; it.IsEnded != wantEnded {
			t.Fatalf("%s: is_ended=%v", it.ID, it.IsEnded)
		}
	}
}

func TestRegionSpotsUnknownRegion(t *testing.T) {
	e := newRegionEnv(t, nil)
	res, b := fetchRegion(t, e, "/v1/regions/busan/spots", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", res.StatusCode)
	}
	var apiErr apiError
	if err := json.Unmarshal(b, &apiErr); err != nil || apiErr.Code != "not_found" {
		t.Fatalf("body %s (%v)", b, err)
	}
}

func TestRegionSpotsGzip(t *testing.T) {
	e := newRegionEnv(t, nil)
	res0, raw := fetchRegion(t, e, seoulPath, nil)
	identityETag := res0.Header.Get("ETag")
	for _, ae := range []string{"gzip", "br;q=1.0, gzip;q=0.8", "*"} {
		res, b := fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": ae})
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("%q: status %d, Content-Encoding %q", ae, res.StatusCode, res.Header.Get("Content-Encoding"))
		}
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain, raw) {
			t.Fatalf("%q: gunzipped body differs from identity body", ae)
		}
		if et := res.Header.Get("ETag"); et == identityETag || !strings.HasPrefix(et, `"`) {
			t.Fatalf("%q: gzip ETag %q must be strong and differ from identity %q", ae, et, identityETag)
		}
	}
	// q=0은 거절
	res, _ := fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": "gzip;q=0"})
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("gzip;q=0 got Content-Encoding %q", res.Header.Get("Content-Encoding"))
	}
}

func TestRegionSpotsNotModified(t *testing.T) {
	e := newRegionEnv(t, nil)
	for _, ae := range []string{"identity", "gzip"} {
		res, _ := fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": ae})
		etag := res.Header.Get("ETag")
		for _, inm := range []string{etag, `"nope", ` + etag, "W/" + etag, "*"} {
			res, b := fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": ae, "If-None-Match": inm})
			if res.StatusCode != http.StatusNotModified || len(b) != 0 {
				t.Fatalf("%s If-None-Match %q: status %d, %d bytes", ae, inm, res.StatusCode, len(b))
			}
			if res.Header.Get("ETag") != etag || res.Header.Get("Cache-Control") != "public, max-age=3600" {
				t.Fatalf("304 headers: ETag %q Cache-Control %q", res.Header.Get("ETag"), res.Header.Get("Cache-Control"))
			}
		}
		res, _ = fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": ae, "If-None-Match": `"stale"`})
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s stale If-None-Match: status %d", ae, res.StatusCode)
		}
	}
	// 다른 인코딩의 ETag로는 304가 아니다(다른 표현).
	resGz, _ := fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": "gzip"})
	res, _ := fetchRegion(t, e, seoulPath, map[string]string{"If-None-Match": resGz.Header.Get("ETag")})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("identity with gzip ETag: status %d", res.StatusCode)
	}
}

// countingReader counts SpotsInBBox calls and can fail them.
type countingReader struct {
	store.SpotReader
	mu    sync.Mutex
	calls int
	fail  bool
}

func (c *countingReader) SpotsInBBox(ctx context.Context, q store.BBoxQuery) ([]domain.Spot, error) {
	c.mu.Lock()
	c.calls++
	fail := c.fail
	c.mu.Unlock()
	if fail {
		return nil, errors.New("db down")
	}
	return c.SpotReader.SpotsInBBox(ctx, q)
}

func (c *countingReader) set(fail bool) (calls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = fail
	return c.calls
}

func TestRegionSpotsCacheRebuild(t *testing.T) {
	var cr *countingReader
	e := newRegionEnv(t, func(m *memory.Store) store.SpotReader {
		cr = &countingReader{SpotReader: m}
		return cr
	})
	res, b := fetchRegion(t, e, seoulPath, nil)
	etag1, body1 := res.Header.Get("ETag"), decodeRegion(t, b)
	for range 5 {
		fetchRegion(t, e, seoulPath, map[string]string{"Accept-Encoding": "gzip"})
	}
	if n := cr.set(false); n != 1 {
		t.Fatalf("store called %d times for 6 requests within TTL, want 1", n)
	}

	// 새 항목이 들어와도 10분 안에는 이전 캐시
	ptr := func(t time.Time) *time.Time { return &t }
	if err := e.store.UpsertSpot(context.Background(), domain.Spot{
		ID: "test:new", Source: "test", SourceID: "new", Title: "new", Category: domain.CategoryPopup,
		Lat: 37.5663, Lng: 126.9779, StartsAt: storetest.Now, EndsAt: ptr(storetest.Now.Add(time.Hour)), FetchedAt: storetest.Now,
	}); err != nil {
		t.Fatal(err)
	}
	e.advance(9 * time.Minute)
	res, b = fetchRegion(t, e, seoulPath, nil)
	if res.Header.Get("ETag") != etag1 || decodeRegion(t, b).Count != body1.Count {
		t.Fatal("cache rebuilt before TTL")
	}

	// 10분이 지나면 다시 만든다 → 새 ETag, 새 generated_at
	e.advance(time.Minute + time.Second)
	res, b = fetchRegion(t, e, seoulPath, map[string]string{"If-None-Match": etag1})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("after TTL with old ETag: status %d", res.StatusCode)
	}
	body2 := decodeRegion(t, b)
	etag2 := res.Header.Get("ETag")
	if etag2 == etag1 || body2.Count != body1.Count+1 || !body2.GeneratedAt.After(body1.GeneratedAt) {
		t.Fatalf("rebuild: etag %q→%q count %d→%d generated_at %v→%v", etag1, etag2, body1.Count, body2.Count, body1.GeneratedAt, body2.GeneratedAt)
	}

	// 다시 만들어도 내용이 같으면 ETag·generated_at 유지 → 304
	e.advance(11 * time.Minute)
	res, _ = fetchRegion(t, e, seoulPath, map[string]string{"If-None-Match": etag2})
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged content after rebuild: status %d", res.StatusCode)
	}
	callsBefore := cr.set(true)

	// 다시 만들기가 실패하면 이전 파일을 계속 주고, 1분 동안은 다시 시도하지 않는다
	e.advance(11 * time.Minute)
	res, _ = fetchRegion(t, e, seoulPath, map[string]string{"If-None-Match": etag2})
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("rebuild failure should serve previous file: status %d", res.StatusCode)
	}
	fetchRegion(t, e, seoulPath, nil)
	if n := cr.set(false); n != callsBefore+1 {
		t.Fatalf("store calls after failure = %d, want %d (no retry within 1 minute)", n, callsBefore+1)
	}
}

func TestRegionSpotsBuildErrorWithoutCache(t *testing.T) {
	e := newRegionEnv(t, func(m *memory.Store) store.SpotReader {
		return &countingReader{SpotReader: m, fail: true}
	})
	res, _ := fetchRegion(t, e, seoulPath, nil)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", res.StatusCode)
	}
}
