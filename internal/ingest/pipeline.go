package ingest

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/geo"
)

// 제외 사유(리포트 키).
const (
	DropEnded       = "ended"
	DropOutOfSeoul  = "out_of_seoul"
	DropNoCoords    = "no_coords"
	DropTooFar      = "too_far_future"
	DropInvalid     = "invalid"
	DropDuplicateID = "duplicate_id"
)

// Pipeline은 소스 → 필터 → 지오코딩 → 중복 제거 → GPT 보강을 한 번 돈다.
type Pipeline struct {
	Sources  []Source
	Geocoder geo.Geocoder // nil이면 좌표 없는 항목은 장소명 색인으로만 보강
	Enricher Enricher     // nil이면 GPT 없이(규칙 결과만)
	// Prior는 이전 결과(content_hash → Spot). 해시가 같으면 요약·분류·무료를 재사용한다.
	Prior       map[string]domain.Spot
	MaxGPTCalls int // 요청 수 상한(배치 하나 = 1회)
	BatchSize   int // 요청 하나에 넣을 항목 수
	HorizonDays int // 시작이 이보다 먼 미래면 제외(0 = 제한 없음)
	Now         func() time.Time
	Log         *slog.Logger
}

// SourceReport는 소스별 실행 결과.
type SourceReport struct {
	Source      string
	Error       string
	Fetched     int
	Kept        int
	Dropped     map[string]int
	Geocoded    int // 네이버 지오코딩으로 좌표를 얻은 수
	PlaceIndex  int // 다른 항목의 같은 장소명 좌표를 빌린 수
	DupesMerged int // 다른 항목에 합쳐져 빠진 수
	CacheHits   int // 해시 캐시로 GPT를 건너뛴 수
	Enriched    int // 이번 실행에서 GPT로 요약을 얻은 수
}

// Report는 실행 전체 결과.
type Report struct {
	Sources          []*SourceReport
	GPTCalls         int
	GPTItems         int
	GPTErrors        int
	GeocodeCalls     int
	PromptTokens     int
	CompletionTokens int
	Total            int
}

// Result는 저장할 Spot과 리포트.
type Result struct {
	Spots  []domain.Spot
	Report Report
}

func (p *Pipeline) log() *slog.Logger {
	if p.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return p.Log
}

// Run은 파이프라인을 한 번 돈다. 소스 하나가 실패해도 나머지는 계속한다(리포트에 에러 기록).
func (p *Pipeline) Run(ctx context.Context) Result {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	var rep Report
	byName := map[string]*SourceReport{}
	var recs []Record
	for _, src := range p.Sources {
		sr := &SourceReport{Source: src.Name(), Dropped: map[string]int{}}
		rep.Sources = append(rep.Sources, sr)
		byName[sr.Source] = sr
		got, fetched, dropped, err := src.Fetch(ctx, now)
		sr.Fetched = fetched
		maps.Copy(sr.Dropped, dropped)
		if err != nil {
			sr.Error = err.Error()
			p.log().Error("ingest: source failed", "source", sr.Source, "err", err)
		}
		recs = append(recs, p.filter(got, now, sr)...)
	}

	geocoded := p.fillCoords(ctx, recs, now, byName, &rep)
	var withCoords []Record
	var geoFlags []bool
	for i, r := range recs {
		sr := byName[r.Spot.Source]
		if !r.HasCoords() {
			sr.Dropped[DropNoCoords]++
			continue
		}
		if !InSeoul(r.Spot.Lat, r.Spot.Lng) {
			sr.Dropped[DropOutOfSeoul]++
			continue
		}
		withCoords = append(withCoords, r)
		geoFlags = append(geoFlags, geocoded[i])
	}

	kept, merged := Dedup(withCoords, geoFlags)
	for s, n := range merged {
		byName[s].DupesMerged += n
	}

	spots := p.enrich(ctx, kept, byName, &rep)
	for _, sp := range spots {
		byName[sp.Source].Kept++
	}
	rep.Total = len(spots)
	return Result{Spots: spots, Report: rep}
}

// filter: 필수 필드·끝남·너무 먼 미래·소스 안 중복 ID를 거른다. 좌표 검사는 지오코딩 뒤에.
func (p *Pipeline) filter(recs []Record, now time.Time, sr *SourceReport) []Record {
	seen := map[string]bool{}
	var out []Record
	for _, r := range recs {
		s := &r.Spot
		s.Title = strings.TrimSpace(s.Title)
		if s.ID == "" || s.Title == "" || s.StartsAt.IsZero() {
			sr.Dropped[DropInvalid]++
			continue
		}
		if seen[s.ID] {
			sr.Dropped[DropDuplicateID]++
			continue
		}
		seen[s.ID] = true
		if s.Ended(now) {
			sr.Dropped[DropEnded]++
			continue
		}
		if p.HorizonDays > 0 && s.StartsAt.After(now.AddDate(0, 0, p.HorizonDays)) {
			sr.Dropped[DropTooFar]++
			continue
		}
		if s.FetchedAt.IsZero() {
			s.FetchedAt = now
		}
		s.Lat, s.Lng = FixCoords(s.Lat, s.Lng)
		if r.HasCoords() && !InSeoul(s.Lat, s.Lng) {
			sr.Dropped[DropOutOfSeoul]++
			continue
		}
		out = append(out, r)
	}
	return out
}

// placeKey는 장소명 색인 키(구 + 정규화 장소명).
func placeKey(gu, place string) string {
	k := keep(strings.ToLower(CleanText(place)))
	if len([]rune(k)) < 3 {
		return ""
	}
	return gu + "|" + k
}

// fillCoords는 좌표 없는 항목에 좌표를 채운다: ① 같은 구·같은 장소명을 가진 다른 항목의 좌표
// ② 네이버 Geocoding(주소, 없으면 "서울특별시 구 장소명"). 질의 결과는 실행 동안 캐시한다.
// 반환값 i번째는 recs[i] 좌표가 원본이 아닌지.
func (p *Pipeline) fillCoords(ctx context.Context, recs []Record, _ time.Time, byName map[string]*SourceReport, rep *Report) []bool {
	geocoded := make([]bool, len(recs))
	index := map[string][2]float64{}
	for _, r := range recs {
		if r.HasCoords() {
			if k := placeKey(r.Gu, r.Spot.PlaceName); k != "" {
				index[k] = [2]float64{r.Spot.Lat, r.Spot.Lng}
			}
		}
	}
	cache := map[string]geo.Point{}
	for i := range recs {
		r := &recs[i]
		if r.HasCoords() {
			continue
		}
		sr := byName[r.Spot.Source]
		if c, ok := index[placeKey(r.Gu, r.Spot.PlaceName)]; ok {
			r.Spot.Lat, r.Spot.Lng = c[0], c[1]
			geocoded[i] = true
			sr.PlaceIndex++
			continue
		}
		if p.Geocoder == nil {
			continue
		}
		for _, q := range geocodeQueries(*r) {
			pt, ok := cache[q]
			if !ok {
				var err error
				rep.GeocodeCalls++
				pt, err = p.Geocoder.Geocode(ctx, q)
				if err != nil {
					p.log().Warn("ingest: geocode failed", "source", r.Spot.Source, "err", err)
					continue
				}
				cache[q] = pt
			}
			if pt.OK && InSeoul(pt.Lat, pt.Lng) {
				r.Spot.Lat, r.Spot.Lng = pt.Lat, pt.Lng
				geocoded[i] = true
				sr.Geocoded++
				break
			}
		}
	}
	return geocoded
}

func geocodeQueries(r Record) []string {
	var qs []string
	if a := strings.TrimSpace(r.AddressQuery); a != "" {
		qs = append(qs, a)
	}
	if pl := strings.TrimSpace(r.Spot.PlaceName); pl != "" {
		qs = append(qs, strings.TrimSpace("서울특별시 "+r.Gu+" "+pl))
	}
	return qs
}

// enrich: 해시 캐시를 먼저 보고, 남은 항목만 GPT 배치로 보낸다(요청 수 상한).
// 진행 중인 항목을 먼저 보강한다(앱에 지금 보이는 것부터).
func (p *Pipeline) enrich(ctx context.Context, recs []Record, byName map[string]*SourceReport, rep *Report) []domain.Spot {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	slices.SortStableFunc(recs, func(a, b Record) int {
		ao, bo := !a.Spot.StartsAt.After(now), !b.Spot.StartsAt.After(now)
		if ao != bo {
			if ao {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Spot.ID, b.Spot.ID)
	})

	spots := make([]domain.Spot, len(recs))
	var queue []int
	for i, r := range recs {
		sp := r.Spot
		sp.ContentHash = ContentHash(r)
		if prev, ok := p.Prior[sp.ContentHash]; ok && prev.Summary != "" {
			sp.Summary = prev.Summary
			if !r.CategorySure && prev.Category.Valid() {
				sp.Category = prev.Category
			}
			if sp.IsFree == nil {
				sp.IsFree = prev.IsFree
			}
			byName[sp.Source].CacheHits++
		} else {
			queue = append(queue, i)
		}
		spots[i] = sp
	}

	batch := p.BatchSize
	if batch <= 0 {
		batch = 10
	}
	for start := 0; start < len(queue) && p.Enricher != nil && rep.GPTCalls < p.MaxGPTCalls; start += batch {
		if ctx.Err() != nil {
			break
		}
		idx := queue[start:min(start+batch, len(queue))]
		items := make([]EnrichItem, 0, len(idx))
		for _, i := range idx {
			r := recs[i]
			items = append(items, EnrichItem{
				ID: r.Spot.ID, Title: r.Spot.Title, SourceCategory: r.SourceCategory, Place: r.Spot.PlaceName,
				Description: Truncate(r.Description, 500), FeeText: Truncate(r.FeeText, 200),
				NeedCategory: !r.CategorySure, NeedFree: r.Spot.IsFree == nil && r.FeeText != "",
			})
		}
		rep.GPTCalls++
		results, usage, err := p.Enricher.EnrichBatch(ctx, items)
		rep.PromptTokens += usage.PromptTokens
		rep.CompletionTokens += usage.CompletionTokens
		if err != nil {
			rep.GPTErrors++
			p.log().Warn("ingest: gpt batch failed", "err", err)
			continue
		}
		byID := map[string]EnrichResult{}
		for _, res := range results {
			byID[res.ID] = res
		}
		for k, i := range idx {
			res, ok := byID[items[k].ID]
			if !ok {
				continue
			}
			r := recs[i]
			src := strings.Join([]string{r.Spot.Title, r.SourceCategory, r.Spot.PlaceName, r.Description, r.FeeText}, " ")
			sp := &spots[i]
			sp.Summary = ValidSummary(res.Summary, src)
			if items[k].NeedCategory && res.Category.Valid() && res.Category != domain.CategoryPopup &&
				(res.Category != domain.CategoryPet || petRe.MatchString(r.Spot.Title+" "+r.Description)) { // pet은 반려동물 언급이 있을 때만(반려식물 오분류 방지)
				sp.Category = res.Category
			}
			if items[k].NeedFree && res.IsFree != nil {
				sp.IsFree = res.IsFree
			}
			if sp.Summary != "" {
				rep.GPTItems++
				byName[sp.Source].Enriched++
			}
		}
	}
	return spots
}

// String은 사람이 읽는 실행 리포트(표). 키·좌표는 들어가지 않는다.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %7s %6s %7s %7s %6s %7s %6s  %s\n",
		"source", "fetched", "kept", "geocode", "placeix", "dupes", "cached", "gpt", "dropped")
	for _, s := range r.Sources {
		var dropped []string
		for _, k := range slices.Sorted(maps.Keys(s.Dropped)) {
			dropped = append(dropped, fmt.Sprintf("%s=%d", k, s.Dropped[k]))
		}
		fmt.Fprintf(&b, "%-18s %7d %6d %7d %7d %6d %7d %6d  %s\n",
			s.Source, s.Fetched, s.Kept, s.Geocoded, s.PlaceIndex, s.DupesMerged, s.CacheHits, s.Enriched, strings.Join(dropped, " "))
		if s.Error != "" {
			fmt.Fprintf(&b, "  error: %s\n", s.Error)
		}
	}
	fmt.Fprintf(&b, "total kept=%d  geocode_calls=%d  gpt_calls=%d (errors %d) gpt_items=%d  tokens in=%d out=%d\n",
		r.Total, r.GeocodeCalls, r.GPTCalls, r.GPTErrors, r.GPTItems, r.PromptTokens, r.CompletionTokens)
	return b.String()
}
