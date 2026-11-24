package ingest

import (
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// DefaultRetainEndedDays는 끝난 볼거리를 보존하는 기간(일). 이 기간 동안은 상세·저장 목록에서
// '종료됨'으로 보여 주고, 지나면 정리한다(DATA-PLAN.md Q1, 이슈 #51).
const DefaultRetainEndedDays = 90

// RetainCutoff는 보존 기준 시각. ends_at이 이보다 이르면 보존 기간이 지난 것이다.
// days가 0 이하면 now(끝난 항목은 모두 버림).
func RetainCutoff(now time.Time, days int) time.Time {
	return now.AddDate(0, 0, -max(days, 0))
}

// Expired는 sp가 보존 기간(days)보다 오래전에 끝났는지.
func Expired(sp domain.Spot, now time.Time, days int) bool {
	return sp.Ended(RetainCutoff(now, days))
}

// MergeRetained는 이번 수집 결과(current)에 이전 결과(previous) 중 이번에 안 보인 항목을 더한다.
// 원천 목록에서 빠졌다는 이유로 지우지 않는다. 단 보존 기간이 지난 항목과 이번에 볼거리가 아니라고
// 판정된 항목(excludedIDs)은 뺀다. 스냅샷 모드에서 DB 모드(upsert만, 삭제 없음)와 같은 의미를 낸다.
func MergeRetained(current, previous []domain.Spot, excludedIDs map[string]bool, now time.Time, days int) (merged []domain.Spot, retained int) {
	seen := make(map[string]bool, len(current))
	merged = make([]domain.Spot, 0, len(current))
	for _, sp := range current {
		seen[sp.ID] = true
		merged = append(merged, sp)
	}
	for _, sp := range previous {
		if seen[sp.ID] || excludedIDs[sp.ID] || Expired(sp, now, days) {
			continue
		}
		seen[sp.ID] = true
		merged = append(merged, sp)
		retained++
	}
	return merged, retained
}
