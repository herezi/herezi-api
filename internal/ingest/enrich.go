package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/herezi/herezi-api/internal/domain"
)

// PromptVersion은 요약 지시의 버전이다. 바뀌면 요약 캐시(SummaryKey)가 무효화된다(요약 규칙을 고칠 때 올린다).
const PromptVersion = "v1"

// RulesVersion은 분류·볼거리 판정 규칙(키워드·매핑·GPT 판정 지시)의 버전이다. 바뀌면 content_hash가
// 바뀌어 GPT 판정(애매한 분류·볼거리 여부)을 다시 받는다. 요약은 SummaryKey가 같으면 재사용한다(이슈 #44).
const RulesVersion = "r2"

// SummaryMaxRunes는 요약 최대 길이(이슈 #42: 60자, 1~2줄).
const SummaryMaxRunes = 60

// EnrichItem은 GPT 한 항목 입력. 원문에 없는 사실을 만들지 않도록 원문 조각만 넘긴다.
type EnrichItem struct {
	ID             string
	Title          string
	SourceCategory string
	Place          string
	Description    string
	FeeText        string
	NeedSummary    bool // 재사용할 요약이 없을 때만
	NeedCategory   bool // 규칙 분류가 애매할 때만
	NeedFree       bool // 규칙 무료 판정이 애매할 때만
	NeedAttraction bool // 규칙으로 볼거리 여부가 애매할 때만
}

// EnrichResult는 GPT 한 항목 출력. Category가 ""이거나 enum 밖이면 버린다. IsFree·IsAttraction nil = 모름.
type EnrichResult struct {
	ID           string
	Summary      string
	Category     domain.Category
	IsFree       *bool
	IsAttraction *bool
}

// Usage는 GPT 토큰 사용량(비용 추정용).
type Usage struct {
	PromptTokens, CompletionTokens int
}

// Enricher는 여러 항목을 한 번의 요청으로 보강한다(internal/ingest/openai).
type Enricher interface {
	EnrichBatch(ctx context.Context, items []EnrichItem) ([]EnrichResult, Usage, error)
}

// ContentHash는 GPT 보강 캐시 키: "규칙 버전-SummaryKey". 같으면 이전 결과(요약·분류·무료·볼거리 판정)를
// 그대로 재사용한다. 규칙 버전만 다르면 요약·무료만 재사용하고 분류·볼거리 판정은 다시 받는다.
func ContentHash(r Record) string { return RulesVersion + "-" + SummaryKey(r) }

// SummaryKeyOf는 content_hash에서 SummaryKey 부분을 꺼낸다. 규칙 버전이 없는 예전 해시(r1 이전)는 그대로.
func SummaryKeyOf(contentHash string) string {
	if _, key, ok := strings.Cut(contentHash, "-"); ok {
		return key
	}
	return contentHash
}

// SummaryKey는 요약 입력 원문의 해시(요약 지시 버전 포함). 예전 content_hash와 같은 값이다.
func SummaryKey(r Record) string {
	h := sha256.New()
	for _, s := range []string{PromptVersion, r.Spot.Title, r.SourceCategory, r.Spot.PlaceName, r.Description, r.FeeText} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

var digitsRe = regexp.MustCompile(`[0-9]+`)

// ValidSummary는 GPT 요약을 정리하고 검증한다(DATA-PLAN.md Q10): 원문에 없는 숫자가 있으면
// 버리고(""), 너무 길면 자른다.
func ValidSummary(summary, source string) string {
	s := strings.TrimSpace(CleanText(summary))
	if s == "" {
		return ""
	}
	for _, d := range digitsRe.FindAllString(s, -1) {
		if !strings.Contains(source, d) {
			return ""
		}
	}
	if len([]rune(s)) > SummaryMaxRunes {
		s = strings.TrimSpace(Truncate(s, SummaryMaxRunes-1)) + "…"
	}
	return s
}
