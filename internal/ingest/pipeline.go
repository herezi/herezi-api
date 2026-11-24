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
	Prior map[string]domain.Spot
	// PriorExcluded는 이전에 GPT가 볼거리 아님으로 판정한 항목(content_hash → 사유). 다시 묻지 않는다.
	PriorExcluded map[string]string
	MaxGPTCalls   int // 요청 수 상한(배치 하나 = 1회)
	BatchSize     int // 요청 하나에 넣을 항목 수
	HorizonDays   int // 시작이 이보다 먼 미래면 제외(0 = 제한 없음)
	Now           func() time.Time
	Log           *slog.Logger
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
	Categories       map[domain.Category]int // 저장한 Spot의 카테고리별 수
}

// DroppedTotals는 소스 전체의 제외 사유별 합계.
func (r Report) DroppedTotals() map[string]int {
	out := map[string]int{}
	for _, s := range r.Sources {
		for k, n := range s.Dropped {
			out[k] += n
		}
	}
	return out
}

// Excluded는 볼거리가 아니라서 뺀 항목(규칙 또는 GPT). GPT 판정분은 다음 실행의 PriorExcluded가 된다.
type Excluded struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Reason      string `json:"reason"`
	ContentHash string `json:"content_hash,omitempty"`
}

// Result는 저장할 Spot과 리포트.
type Result struct {
	Spots    []domain.Spot
	Excluded []Excluded
	Report   Report
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
	rep := Report{Categories: map[domain.Category]int{}}
	byName := map[string]*SourceReport{}
	var recs []Record
	var excluded []Excluded
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
		recs = append(recs, p.filter(got, now, sr, &excluded)...)
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

	spots, gptExcluded := p.enrich(ctx, kept, byName, &rep)
	excluded = append(excluded, gptExcluded...)
	for _, sp := range spots {
		byName[sp.Source].Kept++
		rep.Categories[sp.Category]++
	}
	rep.Total = len(spots)
	return Result{Spots: spots, Excluded: excluded, Report: rep}
}

// filter: 필수 필드·끝남·너무 먼 미래·소스 안 중복 ID·볼거리 아님(제목 규칙)을 거른다.
// 좌표 검사는 지오코딩 뒤에. 볼거리 여부가 애매한 항목은 AttractionMaybe로 표시한다(→ GPT).
func (p *Pipeline) filter(recs []Record, now time.Time, sr *SourceReport, excluded *[]Excluded) []Record {
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
		reason, maybe := AttractionRule(s.Title)
		if reason != "" {
			sr.Dropped[reason]++
			*excluded = append(*excluded, Excluded{ID: s.ID, Title: s.Title, Reason: reason})
			continue
		}
		r.AttractionMaybe = maybe
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
//   - content_hash가 같으면 이전 결과를 그대로 쓴다(GPT 없음).
//   - 규칙 버전만 바뀌었으면(SummaryKey 같음) 요약·무료는 재사용하고, 애매한 분류·볼거리 판정이
//     필요한 항목만 요약 없이 판정만 받는다.
//
// 우선순위: 볼거리 판정 → 새 요약(진행 중 먼저) → 분류 재판정. GPT가 볼거리 아님으로 판정한 항목은
// 빼고 Excluded로 돌려준다. 판정을 못 받은 애매한 항목은 남긴다(다음 실행에서 다시 묻는다).
func (p *Pipeline) enrich(ctx context.Context, recs []Record, byName map[string]*SourceReport, rep *Report) ([]domain.Spot, []Excluded) {
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

	// 요약 캐시: 규칙 버전을 뺀 키 → 이전 Spot.
	bySummary := map[string]domain.Spot{}
	for h, sp := range p.Prior {
		if sp.Summary != "" {
			bySummary[SummaryKeyOf(h)] = sp
		}
	}

	spots := make([]domain.Spot, len(recs))
	items := make([]EnrichItem, len(recs))
	drop := make([]string, len(recs))
	var queue []int
	for i, r := range recs {
		sp := r.Spot
		hash := ContentHash(r)
		sr := byName[sp.Source]
		it := EnrichItem{
			ID: sp.ID, Title: sp.Title, SourceCategory: r.SourceCategory, Place: sp.PlaceName,
			Description: Truncate(r.Description, 500), FeeText: Truncate(r.FeeText, 200),
			NeedSummary: true, NeedCategory: !r.CategorySure, NeedFree: sp.IsFree == nil && r.FeeText != "",
			NeedAttraction: r.AttractionMaybe,
		}
		if reason, ok := p.PriorExcluded[hash]; ok {
			drop[i] = reason
			sr.CacheHits++
			continue
		}
		if prev, ok := p.Prior[hash]; ok && prev.Summary != "" {
			sp.ContentHash = hash
			sp.Summary = prev.Summary
			if !r.CategorySure && AcceptGPTCategory(prev.Category, sp.Title, r.Description) {
				sp.Category = prev.Category
			}
			if sp.IsFree == nil {
				sp.IsFree = prev.IsFree
			}
			sr.CacheHits++
			spots[i] = sp
			continue
		}
		if prev, ok := bySummary[SummaryKey(r)]; ok {
			// 규칙만 바뀜: 요약·무료 재사용. 판정을 못 받으면 예전 해시를 남겨 다음 실행에 다시 묻는다.
			sp.ContentHash = prev.ContentHash
			sp.Summary = prev.Summary
			if sp.IsFree == nil {
				sp.IsFree = prev.IsFree
			}
			if it.NeedCategory && AcceptGPTCategory(prev.Category, sp.Title, r.Description) {
				sp.Category = prev.Category
			}
			it.NeedSummary, it.NeedFree = false, false
			if !it.NeedCategory && !it.NeedAttraction {
				sp.ContentHash = hash
				sr.CacheHits++
				spots[i] = sp
				continue
			}
		}
		if sp.Summary == "" {
			sp.ContentHash = hash // 요약이 없으면 캐시되지 않는다(cmd/ingest는 요약 있는 것만 캐시)
		}
		spots[i], items[i] = sp, it
		queue = append(queue, i)
	}
	prio := func(it EnrichItem) int {
		switch {
		case it.NeedAttraction:
			return 0
		case it.NeedSummary:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(queue, func(a, b int) int { return cmp.Compare(prio(items[a]), prio(items[b])) })

	batch := p.BatchSize
	if batch <= 0 {
		batch = 10
	}
	for start := 0; start < len(queue) && p.Enricher != nil && rep.GPTCalls < p.MaxGPTCalls; start += batch {
		if ctx.Err() != nil {
			break
		}
		idx := queue[start:min(start+batch, len(queue))]
		in := make([]EnrichItem, 0, len(idx))
		for _, i := range idx {
			in = append(in, items[i])
		}
		rep.GPTCalls++
		results, usage, err := p.Enricher.EnrichBatch(ctx, in)
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
		for _, i := range idx {
			it := items[i]
			res, ok := byID[it.ID]
			if !ok {
				continue
			}
			r := recs[i]
			sp := &spots[i]
			sr := byName[sp.Source]
			rep.GPTItems++
			if it.NeedAttraction && res.IsAttraction != nil && !*res.IsAttraction {
				drop[i] = DropGPTNotAttraction
				continue
			}
			if it.NeedCategory && AcceptGPTCategory(res.Category, r.Spot.Title, r.Description) {
				sp.Category = res.Category
			}
			if it.NeedFree && res.IsFree != nil {
				sp.IsFree = res.IsFree
			}
			sp.ContentHash = ContentHash(r)
			if it.NeedSummary {
				src := strings.Join([]string{r.Spot.Title, r.SourceCategory, r.Spot.PlaceName, r.Description, r.FeeText}, " ")
				if sp.Summary = ValidSummary(res.Summary, src); sp.Summary != "" {
					sr.Enriched++
				}
			}
		}
	}

	out := make([]domain.Spot, 0, len(spots))
	var excluded []Excluded
	for i, sp := range spots {
		if drop[i] != "" {
			sr := byName[recs[i].Spot.Source]
			sr.Dropped[drop[i]]++
			excluded = append(excluded, Excluded{ID: recs[i].Spot.ID, Title: recs[i].Spot.Title, Reason: drop[i], ContentHash: ContentHash(recs[i])})
			continue
		}
		out = append(out, sp)
	}
	return out, excluded
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
	var cats, drops []string
	for _, c := range slices.Sorted(maps.Keys(r.Categories)) {
		cats = append(cats, fmt.Sprintf("%s=%d", c, r.Categories[c]))
	}
	totals := r.DroppedTotals()
	for _, k := range slices.Sorted(maps.Keys(totals)) {
		drops = append(drops, fmt.Sprintf("%s=%d", k, totals[k]))
	}
	fmt.Fprintf(&b, "dropped: %s\ncategories: %s\n", strings.Join(drops, " "), strings.Join(cats, " "))
	fmt.Fprintf(&b, "total kept=%d  geocode_calls=%d  gpt_calls=%d (errors %d) gpt_items=%d  tokens in=%d out=%d\n",
		r.Total, r.GeocodeCalls, r.GPTCalls, r.GPTErrors, r.GPTItems, r.PromptTokens, r.CompletionTokens)
	return b.String()
}
