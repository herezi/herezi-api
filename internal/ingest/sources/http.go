// Package sources는 공공데이터 소스별 수집기(ingest.Source 구현)다. 원본 응답을 파싱해
// ingest.Record로 정규화하고, 소스 분류 → 9개 카테고리 규칙 매핑을 둔다(DATA-PLAN.md §5).
//
// 키는 생성자 인자로만 받는다. 요청 URL에 키가 들어가므로 에러 메시지에 URL을 넣지 않는다
// (url.Error를 풀고, 응답 본문도 넣지 않는다).
package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultTimeout은 원본 요청 하나의 제한 시간(서울 1,000건 페이지는 몇 MB).
const DefaultTimeout = 60 * time.Second

func newClient() *http.Client { return &http.Client{Timeout: DefaultTimeout} }

// get은 rawURL을 GET해서 본문을 돌려준다. 에러에 URL(키 포함)을 넣지 않는다.
func get(ctx context.Context, hc *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("build request")
	}
	res, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", res.StatusCode)
	}
	return body, nil
}
