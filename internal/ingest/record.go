// Package ingest는 공공데이터 수집 파이프라인이다(DATA-PLAN.md §6, 이슈 #42).
//
//	Source.Fetch(원본 호출·파싱 → Record) → 필터(끝남·서울 밖·너무 먼 미래) → 좌표 보강(지오코딩)
//	→ 중복 제거(정규화 제목 + 기간 겹침 + 100m) → GPT 보강(요약·애매한 분류·애매한 무료, 해시 캐시) → Sink
//
// 소스별 수집기는 internal/ingest/sources, OpenAI 클라이언트는 internal/ingest/openai에 있고
// cmd/ingest가 조립한다. 이 패키지는 domain·geo만 import한다(httpapi·store 구현체 금지).
package ingest

import (
	"context"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
)

// Record는 소스 하나의 원본 항목을 Spot 형태로 정규화한 것과, 보강·판정에 필요한 원문 조각이다.
type Record struct {
	// Spot은 ID·Source·SourceID·Title·기간·좌표(없으면 0)·장소·이미지·출처·FetchedAt까지 채운다.
	// Category·IsFree는 규칙 판정 결과(아래 Sure 플래그 참고).
	Spot domain.Spot

	// CategorySure가 false면 규칙 매핑이 애매하다는 뜻 → GPT 분류 대상(실패하면 Spot.Category 유지).
	CategorySure bool
	// Spot.IsFree가 nil이고 FeeText가 있으면 GPT 무료 판정 대상.
	FeeText string

	SourceCategory string // 원천 분류(예: "전시/미술", "문화체험>전시/관람") — GPT 입력·리포트용
	Description    string // 요약 입력(태그 제거된 평문)
	Gu             string // 자치구(지오코딩 질의 보조)
	AddressQuery   string // 지오코딩용 주소(없으면 Gu + PlaceName)
}

// HasCoords는 원본에 좌표가 있었는지(0,0이 아닌지).
func (r Record) HasCoords() bool { return r.Spot.Lat != 0 || r.Spot.Lng != 0 }

// Source는 공공데이터 소스 하나.
type Source interface {
	// Name은 Spot.Source 값(예: "seoul_culture").
	Name() string
	// Fetch는 원본을 받아 Record로 정규화한다. now 기준으로 명백히 끝난 항목은 미리 걸러도 된다
	// (상세 호출을 아끼려고). 걸러낸 수는 dropped로 돌려준다.
	Fetch(ctx context.Context, now time.Time) (recs []Record, fetched int, dropped map[string]int, err error)
}
