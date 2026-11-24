// Package openai는 수집 배치의 GPT 보강 클라이언트(ingest.Enricher)다. 표준 net/http로
// Chat Completions를 부르고, 여러 항목을 한 요청에 묶어 JSON으로 받는다.
//
// 조직 공용 월 한도(US$10)가 있으므로 호출 수 상한·해시 캐시는 ingest.Pipeline이 관리한다.
// 키는 Authorization 헤더로만 보내고, 에러 메시지에 응답 본문·키를 넣지 않는다.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

// DefaultBaseURL은 OpenAI API.
const DefaultBaseURL = "https://api.openai.com/v1"

// DefaultModel은 작고 싼 비추론 모델(2026-11-24 /v1/models로 사용 가능 확인, reasoning_effort=none).
const DefaultModel = "gpt-5.4-nano"

// Client는 ingest.Enricher 구현.
type Client struct {
	APIKey          string
	BaseURL         string
	Model           string
	ReasoningEffort string // "none" 등. 빈 값이면 보내지 않는다(비추론 모델)
	HTTP            *http.Client
}

var _ ingest.Enricher = (*Client)(nil)

// New는 기본 모델·엔드포인트로 클라이언트를 만든다.
func New(apiKey string) *Client {
	return &Client{
		APIKey: apiKey, BaseURL: DefaultBaseURL, Model: DefaultModel, ReasoningEffort: "none",
		HTTP: &http.Client{Timeout: 90 * time.Second},
	}
}

const systemPrompt = `너는 서울의 행사·공연·전시 정보를 정리하는 편집자다. 입력 items 각각에 대해 아래 JSON만 출력한다.
{"items":[{"id":"입력 id 그대로","summary":"...","category":"...","is_free":true|false|null}]}
규칙:
- summary: 한국어 1~2줄, 60자 이내. 무엇을 하는/볼 수 있는 곳인지. 입력에 없는 날짜·가격·숫자·장소를 만들지 않는다. 제목을 그대로 반복하지 않는다. 정보가 거의 없으면 제목과 분류로 짧게.
- category: need_category가 true일 때만 채운다(아니면 null). 다음 중 하나: nature(자연·공원·숲·계절 명소), festival(축제·행사·마켓), performance(공연·영화 상영), exhibition(전시·관람), food(먹거리·야시장), family(어린이·가족·체험·교육), pet(반려견·반려묘 등 반려동물 동반·교육. 반려식물은 family), sports(운동·마라톤·생활체육).
- is_free: need_free가 true일 때만 판단한다. 요금 텍스트로 확실히 무료면 true, 유료면 false, 애매하면 null. need_free가 false면 null.`

type promptItem struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	SourceCategory string `json:"source_category,omitempty"`
	Place          string `json:"place,omitempty"`
	Description    string `json:"description,omitempty"`
	Fee            string `json:"fee,omitempty"`
	NeedCategory   bool   `json:"need_category"`
	NeedFree       bool   `json:"need_free"`
}

type chatRequest struct {
	Model           string            `json:"model"`
	Messages        []chatMessage     `json:"messages"`
	ResponseFormat  map[string]string `json:"response_format"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type outItem struct {
	ID       string  `json:"id"`
	Summary  string  `json:"summary"`
	Category *string `json:"category"`
	IsFree   *bool   `json:"is_free"`
}

// EnrichBatch implements ingest.Enricher: items를 한 요청으로 보내고 JSON 결과를 읽는다.
// enum 밖 카테고리는 버린다(검증은 ingest가 한 번 더 한다).
func (c *Client) EnrichBatch(ctx context.Context, items []ingest.EnrichItem) ([]ingest.EnrichResult, ingest.Usage, error) {
	in := make([]promptItem, 0, len(items))
	for _, it := range items {
		in = append(in, promptItem{
			ID: it.ID, Title: it.Title, SourceCategory: it.SourceCategory, Place: it.Place,
			Description: it.Description, Fee: it.FeeText, NeedCategory: it.NeedCategory, NeedFree: it.NeedFree,
		})
	}
	user, err := json.Marshal(map[string]any{"items": in})
	if err != nil {
		return nil, ingest.Usage{}, err
	}
	reqBody, err := json.Marshal(chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(user)},
		},
		ResponseFormat:  map[string]string{"type": "json_object"},
		ReasoningEffort: c.ReasoningEffort,
	})
	if err != nil {
		return nil, ingest.Usage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, ingest.Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, ingest.Usage{}, fmt.Errorf("openai: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, ingest.Usage{}, fmt.Errorf("openai: read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, ingest.Usage{}, fmt.Errorf("openai: http %d", res.StatusCode)
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, ingest.Usage{}, fmt.Errorf("openai: decode: %w", err)
	}
	usage := ingest.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens}
	if len(cr.Choices) == 0 {
		return nil, usage, errors.New("openai: no choices")
	}
	var out struct {
		Items []outItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &out); err != nil {
		return nil, usage, fmt.Errorf("openai: decode content: %w", err)
	}
	results := make([]ingest.EnrichResult, 0, len(out.Items))
	for _, o := range out.Items {
		r := ingest.EnrichResult{ID: o.ID, Summary: strings.TrimSpace(o.Summary), IsFree: o.IsFree}
		if o.Category != nil {
			if cat := domain.Category(strings.TrimSpace(*o.Category)); cat.Valid() {
				r.Category = cat
			}
		}
		results = append(results, r)
	}
	return results, usage, nil
}
