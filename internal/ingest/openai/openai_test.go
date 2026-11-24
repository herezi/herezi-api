package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/ingest"
)

func TestEnrichBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth header missing")
		}
		b, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "test-model" || req.ResponseFormat["type"] != "json_object" || req.ReasoningEffort != "none" {
			t.Errorf("req = %+v", req)
		}
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, `"id":"a"`) ||
			!strings.Contains(req.Messages[1].Content, `"need_free":true`) ||
			!strings.Contains(req.Messages[1].Content, `"need_attraction":true`) {
			t.Errorf("user message = %s", req.Messages[1].Content)
		}
		content := `{"items":[{"id":"a","summary":"덕수궁 돌담길을 밤에 걷는 산책 행사","category":"nature","is_free":true},` +
			`{"id":"b","summary":"요약","category":"bogus","is_free":null,"is_attraction":false}]}`
		resp := map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
			"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 40},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-key")
	c.BaseURL = srv.URL + "/v1"
	c.Model = "test-model"
	res, usage, err := c.EnrichBatch(context.Background(), []ingest.EnrichItem{
		{ID: "a", Title: "덕수궁 야간 산책", NeedCategory: true, NeedFree: true, FeeText: "무료(예약)"},
		{ID: "b", Title: "b", NeedAttraction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 120 || usage.CompletionTokens != 40 {
		t.Fatalf("usage = %+v", usage)
	}
	if len(res) != 2 || res[0].Category != domain.CategoryNature || !*res[0].IsFree || res[0].Summary == "" {
		t.Fatalf("res[0] = %+v", res[0])
	}
	if res[0].IsAttraction != nil || res[1].IsAttraction == nil || *res[1].IsAttraction {
		t.Fatalf("is_attraction = %v / %v", res[0].IsAttraction, res[1].IsAttraction)
	}
	if res[1].Category != "" || res[1].IsFree != nil {
		t.Fatalf("invalid category must be dropped: %+v", res[1])
	}
}

func TestEnrichBatchHTTPErrorHasNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded for key sk-secret"}}`))
	}))
	defer srv.Close()
	c := New("sk-secret")
	c.BaseURL = srv.URL
	_, _, err := c.EnrichBatch(context.Background(), []ingest.EnrichItem{{ID: "a", Title: "t"}})
	if err == nil || strings.Contains(err.Error(), "sk-secret") || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v", err)
	}
}
