package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/herezi/herezi-api/internal/domain"
)

// PromptVersion이 바뀌면 해시가 바뀌어 캐시가 무효화된다(프롬프트를 고칠 때 올린다).
const PromptVersion = "v1"

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
	NeedCategory   bool // 규칙 분류가 애매할 때만
	NeedFree       bool // 규칙 무료 판정이 애매할 때만
}

// EnrichResult는 GPT 한 항목 출력. Category가 ""이거나 enum 밖이면 버린다. IsFree nil = 모름.
type EnrichResult struct {
	ID       string
	Summary  string
	Category domain.Category
	IsFree   *bool
}

// Usage는 GPT 토큰 사용량(비용 추정용).
type Usage struct {
	PromptTokens, CompletionTokens int
}

// Enricher는 여러 항목을 한 번의 요청으로 보강한다(internal/ingest/openai).
type Enricher interface {
	EnrichBatch(ctx context.Context, items []EnrichItem) ([]EnrichResult, Usage, error)
}

// ContentHash는 GPT 입력 원문의 해시. 같으면 이전 결과를 재사용한다.
func ContentHash(r Record) string {
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
