package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/store"
)

// 지역 데이터 파일: GET /v1/regions/{region}/spots (이슈 #74, TECH-PLAN.md §4).
//
// 앱은 사용자 좌표를 서버로 보내지 않는다. 대신 지역 단위 전체 데이터를 받아 두고 거리·근처·지도를
// 기기에서 계산한다. 이 엔드포인트는 좌표를 받지 않으므로 접속 로그에도 좌표가 남지 않는다.
//
// 서버는 요청마다 저장소를 조회하지 않는다. 지역별 응답 본문(원본·gzip)을 메모리에 두고
// 최대 regionCacheTTL마다 다시 만든다. 다시 만든 내용이 같으면 이전 본문·generated_at·ETag를 그대로 써서
// 앱의 If-None-Match가 계속 304를 받게 한다.

const (
	// regionCacheTTL: 캐시를 다시 만드는 최소 간격. 수집은 하루 1회라 수집 뒤 최대 10분 안에 반영된다.
	regionCacheTTL = 10 * time.Minute
	// regionRetryAfterError: 다시 만들기가 실패하면 이전 본문을 계속 주고 이 시간 뒤 다시 시도한다.
	regionRetryAfterError = time.Minute
	// regionBuildTimeout: 저장소 조회 제한 시간(요청이 끊겨도 다시 만들기는 끝낸다).
	regionBuildTimeout   = 20 * time.Second
	regionCacheControl   = "public, max-age=3600"
	regionGzipETagSuffix = "-gz"
)

// regionSpotJSON is components.schemas.RegionSpot: Spot에서 distance_m·saved_at을 뺀 것.
// is_ended는 generated_at 기준이다(앱은 ends_at으로 기기 시각 기준을 다시 계산한다).
type regionSpotJSON struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary,omitempty"`
	Category  domain.Category `json:"category"`
	Lat       float64         `json:"lat"`
	Lng       float64         `json:"lng"`
	Address   string          `json:"address,omitempty"`
	PlaceName string          `json:"place_name,omitempty"`
	StartsAt  time.Time       `json:"starts_at"`
	EndsAt    *time.Time      `json:"ends_at,omitempty"`
	IsFree    *bool           `json:"is_free,omitempty"`
	ImageURL  string          `json:"image_url,omitempty"`
	Source    string          `json:"source"`
	SourceURL string          `json:"source_url,omitempty"`
	FetchedAt time.Time       `json:"fetched_at"`
	IsEnded   bool            `json:"is_ended"`
}

// regionSpotsJSON is components.schemas.RegionSpots.
type regionSpotsJSON struct {
	Region      string           `json:"region"`
	GeneratedAt time.Time        `json:"generated_at"`
	Count       int              `json:"count"`
	Items       []regionSpotJSON `json:"items"`
}

func toRegionSpotJSON(s domain.Spot, now time.Time) regionSpotJSON {
	return regionSpotJSON{
		ID: s.ID, Title: s.Title, Summary: s.Summary, Category: s.Category,
		Lat: s.Lat, Lng: s.Lng, Address: s.Address, PlaceName: s.PlaceName,
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, IsFree: s.IsFree, ImageURL: s.ImageURL,
		Source: s.Source, SourceURL: s.SourceURL, FetchedAt: s.FetchedAt,
		IsEnded: s.Ended(now),
	}
}

// regionFile is one built response.
type regionFile struct {
	raw, gz     []byte
	etag        string // 따옴표 없는 내용 해시(원본 본문의 SHA-256)
	itemsHash   [32]byte
	generatedAt time.Time
	count       int
}

type regionEntry struct {
	file    *regionFile
	nextTry time.Time // 이 시각 전에는 다시 만들지 않는다
}

// regionCache holds built files per region. 다시 만드는 동안 같은 지역 요청은 기다린다(수천 건이라 짧다).
type regionCache struct {
	mu      sync.Mutex
	entries map[string]*regionEntry
}

func newRegionCache() *regionCache { return &regionCache{entries: make(map[string]*regionEntry)} }

// regionQuery는 지역 파일에 넣을 항목을 고르는 저장소 질의다. 목록·지도와 달리 끝난 항목도
// 보존 기간(종료 후 90일) 안이면 넣으므로, SpotFilter.Now(이 시각 전에 끝난 항목 숨김)를 창의 시작으로 둔다.
// Limit 0 = 제한 없음.
func regionQuery(r domain.Region, now time.Time) store.BBoxQuery {
	from, to := domain.RegionWindow(now)
	return store.BBoxQuery{
		SpotFilter: store.SpotFilter{From: from, To: to, Now: from},
		BBox:       r.BBox,
	}
}

// buildRegionFile은 저장소에서 지역 항목을 읽어 본문을 만든다. prev와 항목이 같으면 prev를 돌려준다.
func (s *Server) buildRegionFile(ctx context.Context, r domain.Region, now time.Time, prev *regionFile) (*regionFile, error) {
	spots, err := s.spots.SpotsInBBox(ctx, regionQuery(r, now))
	if err != nil {
		return nil, err
	}
	// 저장소는 영역 중심 거리순으로 준다. 파일은 ID순으로 고정해 같은 데이터면 같은 바이트가 되게 한다.
	slices.SortFunc(spots, func(a, b domain.Spot) int { return strings.Compare(a.ID, b.ID) })
	items := make([]regionSpotJSON, 0, len(spots))
	for _, sp := range spots {
		items = append(items, toRegionSpotJSON(sp, now))
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	itemsHash := sha256.Sum256(itemsJSON)
	if prev != nil && prev.itemsHash == itemsHash {
		return prev, nil
	}
	raw, err := json.Marshal(regionSpotsJSON{Region: r.ID, GeneratedAt: now.UTC(), Count: len(items), Items: items})
	if err != nil {
		return nil, err
	}
	var gz bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &regionFile{
		raw: raw, gz: gz.Bytes(),
		etag:        base64.RawURLEncoding.EncodeToString(sum[:]),
		itemsHash:   itemsHash,
		generatedAt: now.UTC(),
		count:       len(items),
	}, nil
}

// regionFile returns the cached file for r, rebuilding it when the TTL has passed.
// 다시 만들기가 실패하면 이전 파일이 있으면 그것을 주고(에러 로그), 없으면 에러.
func (s *Server) regionFile(ctx context.Context, r domain.Region) (*regionFile, error) {
	c := s.regions
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.now()
	e := c.entries[r.ID]
	if e == nil {
		e = &regionEntry{}
		c.entries[r.ID] = e
	}
	if e.file != nil && now.Before(e.nextTry) {
		return e.file, nil
	}
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), regionBuildTimeout)
	defer cancel()
	f, err := s.buildRegionFile(bctx, r, now, e.file)
	if err != nil {
		e.nextTry = now.Add(regionRetryAfterError)
		if e.file != nil {
			s.log.Error("region file rebuild failed, serving previous", "region", r.ID, "err", logErr(err))
			return e.file, nil
		}
		return nil, fmt.Errorf("build region %s: %w", r.ID, err)
	}
	if f != e.file {
		s.log.Info("region file built", "region", r.ID, "count", f.count, "bytes", len(f.raw), "gzip_bytes", len(f.gz))
	}
	e.file = f
	e.nextTry = now.Add(regionCacheTTL)
	return f, nil
}

func (s *Server) regionSpots(w http.ResponseWriter, r *http.Request) {
	region, ok := domain.RegionByID(r.PathValue("region"))
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "unknown region")
		return
	}
	f, err := s.regionFile(r.Context(), region)
	if err != nil {
		s.log.Error("region spots", "err", logErr(err))
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	useGzip := acceptsGzip(r.Header.Get("Accept-Encoding"))
	// 인코딩이 다르면 다른 표현이므로 강한 ETag도 다르게 준다(RFC 9110 §8.8.3).
	etag := `"` + f.etag + `"`
	body := f.raw
	if useGzip {
		etag = `"` + f.etag + regionGzipETagSuffix + `"`
		body = f.gz
	}
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", regionCacheControl)
	h.Set("Vary", "Accept-Encoding")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	if useGzip {
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// acceptsGzip reports whether Accept-Encoding allows gzip (q=0이면 거절로 본다).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "x-gzip" && coding != "*" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		if q > 0 {
			return true
		}
	}
	return false
}

// etagMatches implements If-None-Match for GET: "*" 또는 목록 중 하나가 약한 비교로 같으면 true.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == etag {
			return true
		}
	}
	return false
}
