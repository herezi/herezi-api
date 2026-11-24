package ingest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/geo"
)

func TestParseRange(t *testing.T) {
	st, end, ok := ParseRange("2026-11-01 00:00:00.0", "2026-11-30 00:00:00.0")
	if !ok || !st.Equal(time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)) ||
		!end.Equal(time.Date(2026, 11, 30, 14, 59, 59, 0, time.UTC)) {
		t.Fatalf("st=%v end=%v", st, end)
	}
	_, end, _ = ParseRange("20261210", "20261210 ")
	if end == nil || end.Hour() != 14 || end.Minute() != 59 {
		t.Fatalf("date-only end must be 23:59:59 KST: %v", end)
	}
	_, end, ok = ParseRange("2026.12.01", "")
	if !ok || end != nil {
		t.Fatalf("missing end → nil, got %v %v", end, ok)
	}
	if _, _, ok := ParseRange("상시", "2026-12-01"); ok {
		t.Fatal("unparsable start must fail")
	}
	// 시각이 있는 종료는 그대로, 시작보다 이르면 시작으로
	st, end, _ = ParseRange("2026-11-01 10:00:00", "2026-10-01 18:00:00")
	if !end.Equal(st) {
		t.Fatalf("end before start: %v %v", st, end)
	}
}

func TestDetectFree(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		in   string
		want *bool
	}{
		{"무료", &yes},
		{"전석 무료(사전 예약)", &yes},
		{"R 90,000원 / S 70,000원", &no},
		{"회당 35,000원", &no},
		{"", nil},
		{"성인 5,000원 / 어린이 무료", nil},
		{"무료 (일부 체험 유료)", nil},
		{"별도 문의", nil},
		{"0원", nil},
	} {
		got := DetectFree(tc.in)
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("DetectFree(%q) = %v, want %v", tc.in, deref(got), deref(tc.want))
		}
	}
}

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"<p>안녕&nbsp;하세요</p><br/>반가워요": "안녕 하세요 반가워요",
		"연극 &amp;lt;햄릿&amp;gt;":       "연극 <햄릿>",
		"<One Voice> 송년":              "<One Voice> 송년",
		"  a \r\n\t b ":               "a b",
	} {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoords(t *testing.T) {
	if !InSeoul(37.5663, 126.9779) || InSeoul(35.18, 129.07) || InSeoul(0, 0) {
		t.Fatal("InSeoul")
	}
	if lat, lng := FixCoords(126.97, 37.56); lat != 37.56 || lng != 126.97 {
		t.Fatal("swapped coords must be fixed")
	}
}

func TestNormalizeTitle(t *testing.T) {
	if a, b := NormalizeTitle("[대학로] 연극 <오백에 삼십> 2026"), NormalizeTitle("연극 오백에 삼십"); a != "연극" && !sameTitle(a, b) {
		t.Fatalf("%q vs %q", a, b)
	}
	if k := NormalizeTitle("[세 개의 심장]"); k != "세개의심장" {
		t.Fatalf("bracket-only title keeps content: %q", k)
	}
	if !sameTitle(NormalizeTitle("2026 서울빛초롱축제"), NormalizeTitle("서울빛초롱축제")) {
		t.Fatal("year must be ignored")
	}
	if sameTitle("전시", "특별전시회") {
		t.Fatal("short keys must not match by containment")
	}
}

var now = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

func rec(source, id, title string, lat, lng float64, start, end time.Time) Record {
	return Record{Spot: domain.Spot{
		ID: domain.SpotID(source, id), Source: source, SourceID: id, Title: title,
		Category: domain.CategoryFestival, Lat: lat, Lng: lng, StartsAt: start, EndsAt: &end,
	}, CategorySure: true}
}

func TestDedup(t *testing.T) {
	d := 24 * time.Hour
	a := rec("cultureinfo", "1", "연극 <오백에 삼십>", 37.57664, 127.00393, now.Add(-d), now.Add(30*d))
	b := rec("seoul_culture", "2", "[대학로] 연극 <오백에 삼십>", 37.57670, 127.00400, now, now.Add(10*d))
	b.Spot.ImageURL = "https://img"
	a.Spot.IsFree = new(bool)
	c := rec("seoul_culture", "3", "늘근도둑이야기", 37.57664, 127.00393, now, now.Add(d))                      // 같은 장소, 다른 제목
	e := rec("tourapi", "4", "연극 <오백에 삼십>", 37.5900, 127.00393, now, now.Add(d))                         // 1.5km 떨어짐
	f := rec("seoul_reservation", "5", "연극 <오백에 삼십>", 37.57664, 127.00393, now.Add(60*d), now.Add(61*d)) // 기간 안 겹침
	kept, merged := Dedup([]Record{a, b, c, e, f}, make([]bool, 5))
	if len(kept) != 4 || merged["cultureinfo"] != 1 {
		t.Fatalf("kept=%d merged=%v", len(kept), merged)
	}
	if kept[0].Spot.ID != "seoul_culture:2" || kept[0].Spot.IsFree == nil {
		t.Fatalf("이미지 있는 쪽이 대표, 빠진 필드는 채움: %+v", kept[0].Spot)
	}
}

func TestDedupPrefersNativeCoords(t *testing.T) {
	a := rec("seoul_culture", "1", "겨울 축제", 37.5663, 126.9779, now, now.Add(time.Hour))
	b := rec("tourapi", "2", "겨울 축제", 37.5664, 126.9779, now, now.Add(time.Hour))
	kept, _ := Dedup([]Record{a, b}, []bool{true, false})
	if len(kept) != 1 || kept[0].Spot.ID != "tourapi:2" {
		t.Fatalf("kept = %+v", kept)
	}
}

func TestValidSummary(t *testing.T) {
	src := "국립국악원 송년음악회 2026 S석 50,000원"
	if got := ValidSummary("국립국악원에서 열리는 송년 국악 공연", src); got == "" {
		t.Fatal("valid summary dropped")
	}
	if got := ValidSummary("12월 31일 단 하루 공연", src); got != "" {
		t.Fatalf("summary with invented numbers must be dropped: %q", got)
	}
	long := strings.Repeat("가", 80)
	if got := ValidSummary(long, src); len([]rune(got)) != SummaryMaxRunes {
		t.Fatalf("len = %d", len([]rune(got)))
	}
}

func TestContentHashStable(t *testing.T) {
	a := rec("s", "1", "제목", 0, 0, now, now)
	b := a
	b.Spot.FetchedAt = now.Add(time.Hour) // 수집 시각은 해시에 안 들어감
	if ContentHash(a) != ContentHash(b) {
		t.Fatal("hash must ignore fetched_at")
	}
	b.Description = "바뀐 설명"
	if ContentHash(a) == ContentHash(b) {
		t.Fatal("hash must change with description")
	}
}

// --- pipeline ---

type fakeSource struct {
	name string
	recs []Record
	err  error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Fetch(context.Context, time.Time) ([]Record, int, map[string]int, error) {
	return f.recs, len(f.recs), map[string]int{"not_leisure": 1}, f.err
}

type fakeGeocoder map[string]geo.Point

func (g fakeGeocoder) Geocode(_ context.Context, q string) (geo.Point, error) { return g[q], nil }

type fakeEnricher struct {
	calls int
	seen  []EnrichItem
}

func (f *fakeEnricher) EnrichBatch(_ context.Context, items []EnrichItem) ([]EnrichResult, Usage, error) {
	f.calls++
	f.seen = append(f.seen, items...)
	var out []EnrichResult
	yes := true
	for _, it := range items {
		r := EnrichResult{ID: it.ID, Summary: it.Title + " 요약", Category: domain.CategoryNature, IsFree: &yes}
		out = append(out, r)
	}
	return out, Usage{PromptTokens: 100, CompletionTokens: 20}, nil
}

func TestPipeline(t *testing.T) {
	d := 24 * time.Hour
	ongoing := rec("seoul_culture", "1", "서울광장 겨울 축제", 37.5663, 126.9779, now.Add(-d), now.Add(5*d))
	ongoing.CategorySure = false
	ongoing.FeeText = "무료(일부 유료)"
	ended := rec("seoul_culture", "2", "지난 전시", 37.57, 126.98, now.Add(-10*d), now.Add(-d))
	busan := rec("seoul_culture", "3", "부산 축제", 35.18, 129.07, now, now.Add(d))
	far := rec("seoul_culture", "4", "내년 축제", 37.57, 126.98, now.Add(200*d), now.Add(201*d))
	noCoords := rec("cultureinfo", "5", "주소만 있는 전시", 0, 0, now.Add(time.Hour), now.Add(d))
	noCoords.AddressQuery = "서울특별시 중구 세종대로 110"
	placeOnly := rec("cultureinfo", "6", "장소명만 있는 공연", 0, 0, now.Add(time.Hour), now.Add(d))
	placeOnly.Spot.PlaceName, placeOnly.Gu = "국립국악원 예악당", "서초구"
	withPlace := rec("seoul_culture", "7", "국악 송년회", 37.4777, 127.0098, now.Add(time.Hour), now.Add(d))
	withPlace.Spot.PlaceName, withPlace.Gu = "국립국악원 예악당", "서초구"
	lost := rec("cultureinfo", "8", "좌표 못 찾음", 0, 0, now, now.Add(d))
	lost.Spot.PlaceName = "어딘가"
	dup := rec("cultureinfo", "9", "서울광장 겨울축제", 37.5664, 126.9780, now, now.Add(d))
	cached := rec("seoul_culture", "10", "캐시된 전시", 37.58, 126.98, now, now.Add(d))

	enr := &fakeEnricher{}
	p := &Pipeline{
		Sources: []Source{
			fakeSource{name: "seoul_culture", recs: []Record{ongoing, ended, busan, far, withPlace, cached}},
			fakeSource{name: "cultureinfo", recs: []Record{noCoords, placeOnly, lost, dup}},
			fakeSource{name: "tourapi", err: errors.New("boom")},
		},
		Geocoder:    fakeGeocoder{"서울특별시 중구 세종대로 110": {Lat: 37.5666, Lng: 126.9784, OK: true}},
		Enricher:    enr,
		Prior:       map[string]domain.Spot{ContentHash(cached): {Summary: "이전 요약"}},
		MaxGPTCalls: 1,
		BatchSize:   2,
		HorizonDays: 90,
		Now:         func() time.Time { return now },
	}
	res := p.Run(context.Background())
	rep := res.Report
	sc, ci, ta := rep.Sources[0], rep.Sources[1], rep.Sources[2]

	if sc.Dropped[DropEnded] != 1 || sc.Dropped[DropOutOfSeoul] != 1 || sc.Dropped[DropTooFar] != 1 || sc.Dropped["not_leisure"] != 1 {
		t.Errorf("seoul dropped = %v", sc.Dropped)
	}
	if ci.Geocoded != 1 || ci.PlaceIndex != 1 || ci.Dropped[DropNoCoords] != 1 || ci.DupesMerged != 1 {
		t.Errorf("cultureinfo = %+v", ci)
	}
	if ta.Error != "boom" {
		t.Errorf("tourapi error = %q", ta.Error)
	}
	if rep.Total != 5 || len(res.Spots) != 5 || sc.Kept != 3 || ci.Kept != 2 {
		t.Fatalf("total=%d sc.kept=%d ci.kept=%d", rep.Total, sc.Kept, ci.Kept)
	}
	if rep.GPTCalls != 1 || enr.calls != 1 || len(enr.seen) != 2 || sc.CacheHits != 1 {
		t.Errorf("gpt calls=%d items=%d cache=%d (상한 1회 × 2건)", rep.GPTCalls, len(enr.seen), sc.CacheHits)
	}
	if rep.PromptTokens != 100 {
		t.Errorf("tokens = %d", rep.PromptTokens)
	}
	byID := map[string]domain.Spot{}
	for _, s := range res.Spots {
		byID[s.ID] = s
		if s.ContentHash == "" {
			t.Errorf("%s: content_hash empty", s.ID)
		}
	}
	// 진행 중 항목이 먼저 GPT로 간다: 애매한 분류·무료만 GPT 결과를 쓴다.
	o := byID["seoul_culture:1"]
	if o.Summary == "" || o.Category != domain.CategoryNature || !o.Free() {
		t.Errorf("ongoing enriched = %+v", o)
	}
	if !enr.seen[0].NeedCategory || !enr.seen[0].NeedFree {
		t.Errorf("first item flags = %+v", enr.seen[0])
	}
	if c := byID["seoul_culture:10"]; c.Summary != "이전 요약" {
		t.Errorf("cache hit summary = %q", c.Summary)
	}
	if out := rep.String(); !strings.Contains(out, "seoul_culture") || !strings.Contains(out, "gpt_calls=1") {
		t.Errorf("report = %s", out)
	}
}

type fakePlaces struct {
	byQuery map[string][]geo.Place
	calls   int
}

func (f *fakePlaces) SearchPlace(_ context.Context, q string) ([]geo.Place, error) {
	f.calls++
	if q == "에러 나는 곳 종로구" {
		return nil, errors.New("boom")
	}
	return f.byQuery[q], nil
}

func TestPipelinePlaceSearchFallback(t *testing.T) {
	d := 24 * time.Hour
	mk := func(id, place, gu string) Record {
		r := rec("cultureinfo", id, "공연 "+id, 0, 0, now.Add(time.Hour), now.Add(d))
		r.Spot.PlaceName, r.Gu = place, gu
		return r
	}
	addr := mk("1", "세종문화회관 대극장", "종로구")
	addr.AddressQuery = "서울특별시 종로구 세종대로 175"
	kakao := mk("2", "국립국악원 예악당", "서초구")
	again := mk("3", "국립국악원 예악당", "서초구")  // 같은 장소: 색인 재사용(카카오 재호출 없음)
	unlike := mk("4", "작은 책방 모퉁이", "마포구") // 이름이 다른 후보만 → 탈락
	outside := mk("5", "경기 아트센터", "")     // 서울 밖 후보 → 탈락
	failing := mk("6", "에러 나는 곳", "종로구")
	places := &fakePlaces{byQuery: map[string][]geo.Place{
		"국립국악원 예악당 서초구": {{Name: "국립국악원 예악당", Lat: 37.4780, Lng: 127.0087}},
		"작은 책방 모퉁이 마포구": {{Name: "마포구청", Lat: 37.5662, Lng: 126.9016}},
		"경기 아트센터":       {{Name: "경기아트센터", Lat: 37.2610, Lng: 127.0300}},
	}}
	p := &Pipeline{
		Sources:  []Source{fakeSource{name: "cultureinfo", recs: []Record{addr, kakao, again, unlike, outside, failing}}},
		Geocoder: fakeGeocoder{"서울특별시 종로구 세종대로 175": {Lat: 37.5725, Lng: 126.9757, OK: true}},
		Places:   places,
		Now:      func() time.Time { return now },
	}
	res := p.Run(context.Background())
	ci := res.Report.Sources[0]
	if ci.Geocoded != 1 || ci.PlaceSearch != 1 || ci.PlaceIndex != 1 || ci.Dropped[DropNoCoords] != 3 {
		t.Errorf("report = %+v", ci)
	}
	if places.calls != 5 || res.Report.PlaceCalls != 5 {
		t.Errorf("kakao calls = %d / %d (네이버로 찾은 항목·색인 재사용은 카카오를 부르지 않고, '작은 책방 모퉁이'는 '작은 책방'까지 줄여 2회)", places.calls, res.Report.PlaceCalls)
	}
	byID := map[string]domain.Spot{}
	for _, s := range res.Spots {
		byID[s.ID] = s
	}
	if s := byID["cultureinfo:2"]; s.Lat != 37.4780 || s.Lng != 127.0087 {
		t.Errorf("kakao coords = %v,%v", s.Lat, s.Lng)
	}
	if !strings.Contains(res.Report.String(), "kakao") {
		t.Errorf("report header missing kakao column")
	}
}

func TestSimilarPlaceName(t *testing.T) {
	cases := []struct {
		want, got string
		ok        bool
	}{
		{"국립국악원 예악당", "국립국악원 예악당", true},
		{"국립국악원 예악당", "국립국악원", true},          // 포함
		{"예술의전당 오페라극장", "예술의전당 오페라하우스", true}, // bigram
		{"북서울꿈의숲", "북서울 꿈의숲 아트센터", true},
		{"작은 책방 모퉁이", "마포구청", false},
		{"공원", "서울숲공원", false}, // 너무 짧음 + 유사도 낮음
		{"", "아무곳", false},
	}
	for _, c := range cases {
		if got := SimilarPlaceName(c.want, c.got); got != c.ok {
			t.Errorf("SimilarPlaceName(%q, %q) = %v, want %v", c.want, c.got, got, c.ok)
		}
	}
}

func TestPlaceNames(t *testing.T) {
	cases := map[string][]string{
		"코엑스 2층 아셈볼룸":              {"코엑스 2층 아셈볼룸", "코엑스 2층", "코엑스"},
		"거점 4호 우리동네키움센터 (4층 미디어실)": {"거점 4호 우리동네키움센터", "거점 4호"},
		"서울특별시 중구 청계천 청계광장 일대":     {"중구 청계천 청계광장", "중구 청계천"},
		"허준박물관 등":                  {"허준박물관"},
		"4층 시네마라운지":                {"4층 시네마라운지"},
		"":                         nil,
		"갈산문화예술센터 아이누리홀":           {"갈산문화예술센터 아이누리홀", "갈산문화예술센터"},
	}
	for in, want := range cases {
		if got := placeNames(in); !slices.Equal(got, want) {
			t.Errorf("placeNames(%q) = %q, want %q", in, got, want)
		}
	}
}
