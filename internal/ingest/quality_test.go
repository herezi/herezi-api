package ingest

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// qualityFixture는 testdata/quality_titles.json(실제 수집 제목, 이슈 #44).
type qualityFixture struct {
	Items []struct {
		Title          string `json:"title"`
		Want           string `json:"want"`
		SourceCategory string `json:"source_category"`
		WantCategory   string `json:"want_category"`
	} `json:"items"`
}

func loadQualityFixture(t *testing.T) qualityFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/quality_titles.json")
	if err != nil {
		t.Fatal(err)
	}
	var f qualityFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Items) < 30 {
		t.Fatalf("fixture has %d items, want >= 30", len(f.Items))
	}
	return f
}

func TestAttractionRuleFixture(t *testing.T) {
	for _, it := range loadQualityFixture(t).Items {
		reason, maybe := AttractionRule(it.Title)
		got := reason
		switch {
		case reason == "" && maybe:
			got = "gpt"
		case reason == "":
			got = "keep"
		}
		if got != it.Want {
			t.Errorf("%q = %s, want %s", it.Title, got, it.Want)
		}
	}
}

func TestAcceptGPTCategory(t *testing.T) {
	for _, tc := range []struct {
		c           domain.Category
		title, desc string
		want        bool
	}{
		{domain.CategoryPet, "강북구 스마트팜 센터 반려식물 클리닉", "반려동물처럼 식물을 돌봐요", false},
		{domain.CategoryPet, "반려식물교실 11월(실습)", "", false},
		{domain.CategoryPet, "반려견 산책교실 (월드컵공원)", "", true},
		{domain.CategorySports, "동대문구보건소 심폐소생술교육", "", false},
		{domain.CategorySports, "[12월 석촌호수 아뜰리에 교육 프로그램] 필라테스", "", true},
		{domain.CategoryPopup, "성수 팝업", "", false},
		{domain.CategoryPerformance, "명동역사문화투어(중구 도보관광 프로그램)", "", false},
		{domain.CategoryPerformance, "2026 한성백제박물관 콘서트 4회차 예약 안내", "", true},
		{domain.CategoryFamily, "가족 탐조교실", "", true},
		{"bogus", "x", "", false},
	} {
		if got := AcceptGPTCategory(tc.c, tc.title, tc.desc); got != tc.want {
			t.Errorf("%s %q = %v, want %v", tc.c, tc.title, got, tc.want)
		}
	}
}

func TestContentHashHasRulesVersion(t *testing.T) {
	r := rec("s", "1", "제목", 0, 0, now, now)
	h := ContentHash(r)
	if h != RulesVersion+"-"+SummaryKey(r) || SummaryKeyOf(h) != SummaryKey(r) {
		t.Fatalf("hash = %q", h)
	}
	if SummaryKeyOf("5552a18399209bf228646835") != "5552a18399209bf228646835" { // r1 이전 해시
		t.Fatal("legacy hash must be its own summary key")
	}
}

// judgeEnricher는 제목으로 볼거리 판정을 흉내 낸다("모집"이 있으면 볼거리 아님).
type judgeEnricher struct{ seen []EnrichItem }

func (j *judgeEnricher) EnrichBatch(_ context.Context, items []EnrichItem) ([]EnrichResult, Usage, error) {
	j.seen = append(j.seen, items...)
	var out []EnrichResult
	for _, it := range items {
		r := EnrichResult{ID: it.ID, Category: domain.CategoryPet}
		if it.NeedSummary {
			r.Summary = "새 요약"
		}
		if it.NeedAttraction {
			ok := !strings.Contains(it.Title, "모집")
			r.IsAttraction = &ok
		}
		out = append(out, r)
	}
	return out, Usage{}, nil
}

func TestPipelineAttraction(t *testing.T) {
	d := 24 * time.Hour
	mk := func(id, title string) Record {
		r := rec("seoul_reservation", id, title, 37.57, 126.98+float64(len(id))*0.01, now.Add(-d), now.Add(d))
		r.CategorySure = true
		return r
	}
	recruit := mk("1", "[서울시] 2026년 서울청년문화패스 신청자 모집")
	maybeNo := mk("22", "한강 생태 유아 놀이 프로그램 모집")
	maybeYes := mk("333", "(고덕)(토)겨울철새 맞이 가족 탐조교실")
	plant := mk("4444", "반려식물교실 11월(실습)")
	plant.CategorySure = false
	plant.Spot.Category = domain.CategoryFamily
	cachedNo := mk("55555", "성동구_매봉산유아숲체험원 수시 모집(주중/평일/오후반)")

	enr := &judgeEnricher{}
	p := &Pipeline{
		Sources:       []Source{fakeSource{name: "seoul_reservation", recs: []Record{recruit, maybeNo, maybeYes, plant, cachedNo}}},
		Enricher:      enr,
		PriorExcluded: map[string]string{ContentHash(cachedNo): DropGPTNotAttraction},
		MaxGPTCalls:   5,
		BatchSize:     10,
		Now:           func() time.Time { return now },
	}
	res := p.Run(context.Background())
	sr := res.Report.Sources[0]
	if sr.Dropped[DropRecruit] != 1 || sr.Dropped[DropGPTNotAttraction] != 2 {
		t.Fatalf("dropped = %v", sr.Dropped)
	}
	if len(res.Spots) != 2 || len(enr.seen) != 3 { // 규칙 제외·캐시 제외는 GPT에 안 간다
		t.Fatalf("spots=%d seen=%d", len(res.Spots), len(enr.seen))
	}
	if !enr.seen[0].NeedAttraction { // 볼거리 판정이 먼저
		t.Errorf("first item = %+v", enr.seen[0])
	}
	for _, s := range res.Spots {
		if s.Category == domain.CategoryPet {
			t.Errorf("%q must not be pet", s.Title)
		}
	}
	var gptEx int
	for _, e := range res.Excluded {
		if e.Reason == DropGPTNotAttraction && e.ContentHash != "" {
			gptEx++
		}
	}
	if gptEx != 2 || res.Report.Total != 2 || res.Report.Categories[domain.CategoryFestival] != 1 {
		t.Errorf("excluded=%+v report=%+v", res.Excluded, res.Report)
	}
}

// 규칙 버전만 바뀐 이전 결과: 요약·무료는 재사용하고, 애매한 분류·볼거리 판정만 다시 받는다.
func TestPipelineRulesVersionChange(t *testing.T) {
	d := 24 * time.Hour
	sure := rec("seoul_culture", "1", "국립중앙박물관 기증관 재개관", 37.52, 126.98, now.Add(-d), now.Add(d))
	unsure := rec("seoul_reservation", "2", "반려식물교실 11월(실습)", 37.55, 127.0, now.Add(-d), now.Add(d))
	unsure.CategorySure = false
	unsure.Spot.Category = domain.CategoryFamily
	yes := true
	legacy := func(r Record, cat domain.Category) domain.Spot { // r1 이전 해시(규칙 버전 없음)
		return domain.Spot{ContentHash: SummaryKey(r), Summary: "이전 요약", Category: cat, IsFree: &yes}
	}
	enr := &judgeEnricher{}
	p := &Pipeline{
		Sources:  []Source{fakeSource{name: "seoul_culture", recs: []Record{sure}}, fakeSource{name: "seoul_reservation", recs: []Record{unsure}}},
		Enricher: enr,
		Prior: map[string]domain.Spot{
			SummaryKey(sure): legacy(sure, domain.CategoryExhibition), SummaryKey(unsure): legacy(unsure, domain.CategoryPet),
		},
		MaxGPTCalls: 5,
		Now:         func() time.Time { return now },
	}
	res := p.Run(context.Background())
	if len(enr.seen) != 1 || enr.seen[0].ID != unsure.Spot.ID || enr.seen[0].NeedSummary || !enr.seen[0].NeedCategory {
		t.Fatalf("seen = %+v", enr.seen)
	}
	for _, s := range res.Spots {
		if s.Summary != "이전 요약" || !s.Free() || s.ContentHash != RulesVersion+"-"+SummaryKeyOf(s.ContentHash) {
			t.Errorf("%s = %+v", s.ID, s)
		}
		if s.ID == unsure.Spot.ID && s.Category != domain.CategoryFamily { // 예전 GPT pet 판정은 버린다
			t.Errorf("unsure category = %s", s.Category)
		}
	}

	// GPT 상한 0: 판정을 못 받으면 예전 해시를 남겨 다음 실행에 다시 묻는다.
	enr.seen = nil
	p.MaxGPTCalls = 0
	res = p.Run(context.Background())
	for _, s := range res.Spots {
		if s.ID == unsure.Spot.ID && s.ContentHash != SummaryKey(unsure) {
			t.Errorf("unjudged hash = %q", s.ContentHash)
		}
	}
}
