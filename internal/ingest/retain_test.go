package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

func TestPipelineRetainsRecentlyEnded(t *testing.T) {
	d := 24 * time.Hour
	live := rec("seoul_culture", "1", "진행 중 전시", 37.57, 126.98, now.Add(-d), now.Add(5*d))
	recent := rec("seoul_culture", "2", "지난주 끝난 전시", 37.57, 126.98, now.Add(-20*d), now.Add(-7*d))
	old := rec("seoul_culture", "3", "넉 달 전 끝난 전시", 37.57, 126.98, now.Add(-150*d), now.Add(-120*d))
	recent.CategorySure = false // 애매해도 끝난 항목은 GPT로 보내지 않는다
	enr := &fakeEnricher{}
	p := &Pipeline{
		Sources:         []Source{fakeSource{name: "seoul_culture", recs: []Record{live, recent, old}}},
		Enricher:        enr,
		MaxGPTCalls:     10,
		RetainEndedDays: 90,
		Now:             func() time.Time { return now },
	}
	res := p.Run(context.Background())
	sc := res.Report.Sources[0]
	if res.Report.Total != 2 || res.Report.EndedKept != 1 || sc.Dropped[DropEnded] != 1 {
		t.Fatalf("total=%d ended_kept=%d dropped=%v", res.Report.Total, res.Report.EndedKept, sc.Dropped)
	}
	if len(enr.seen) != 1 || enr.seen[0].ID != live.Spot.ID {
		t.Errorf("gpt items = %+v (끝난 항목은 GPT 제외)", enr.seen)
	}

	// 보존 기간 0이면 예전처럼 끝난 항목은 모두 버린다.
	p.RetainEndedDays = 0
	if res := p.Run(context.Background()); res.Report.Total != 1 {
		t.Errorf("retain 0: total = %d", res.Report.Total)
	}
}

func TestMergeRetained(t *testing.T) {
	d := 24 * time.Hour
	end := func(t time.Time) *time.Time { return &t }
	cur := []domain.Spot{{ID: "a", Title: "이번 수집", StartsAt: now, EndsAt: end(now.Add(d))}}
	prev := []domain.Spot{
		{ID: "a", Title: "예전 a", StartsAt: now},                                                 // 이번 것이 우선
		{ID: "b", Title: "원천에서 빠짐(진행 중)", StartsAt: now.Add(-d)},                                // 유지
		{ID: "c", Title: "30일 전 끝남", StartsAt: now.Add(-40 * d), EndsAt: end(now.Add(-30 * d))}, // 유지
		{ID: "d", Title: "100일 전 끝남", StartsAt: now.Add(-110 * d), EndsAt: end(now.Add(-100 * d))},
		{ID: "e", Title: "이번에 볼거리 아님 판정", StartsAt: now},
	}
	got, retained := MergeRetained(cur, prev, map[string]bool{"e": true}, now, 90)
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if retained != 2 || len(got) != 3 || ids[0] != "a" || got[0].Title != "이번 수집" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("ids=%v retained=%d", ids, retained)
	}
}

func TestExpired(t *testing.T) {
	e := now.Add(-91 * 24 * time.Hour)
	sp := domain.Spot{EndsAt: &e}
	if !Expired(sp, now, 90) || Expired(sp, now, 100) || Expired(domain.Spot{}, now, 0) {
		t.Fatal("Expired")
	}
	if !RetainCutoff(now, -5).Equal(now) {
		t.Fatal("negative days")
	}
}
