package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// NaverBaseURL은 네이버 클라우드 Maps Reverse Geocoding 엔드포인트.
const NaverBaseURL = "https://maps.apigw.ntruss.com/map-reversegeocode/v2/gc"

// NaverTimeout은 네이버 호출 한 번의 제한 시간.
const NaverTimeout = 2 * time.Second

// Naver는 네이버 클라우드 Maps Reverse Geocoding 클라이언트.
// Client Secret이 필요하므로 앱이 아니라 서버에서만 부른다.
type Naver struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
}

// NewNaver는 기본 엔드포인트·2초 타임아웃으로 클라이언트를 만든다.
func NewNaver(clientID, clientSecret string) *Naver {
	return &Naver{
		BaseURL:      NaverBaseURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTP:         &http.Client{Timeout: NaverTimeout},
	}
}

type naverResponse struct {
	Status struct {
		Code    int    `json:"code"`
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"status"`
	Results []struct {
		Name   string `json:"name"`
		Region struct {
			Area1 naverArea `json:"area1"`
			Area2 naverArea `json:"area2"`
			Area3 naverArea `json:"area3"`
		} `json:"region"`
	} `json:"results"`
}

type naverArea struct {
	Name string `json:"name"`
}

// 네이버 status.code: 0 성공, 3 결과 없음.
const (
	naverStatusOK       = 0
	naverStatusNoResult = 3
)

// Reverse는 좌표의 행정구역을 돌려준다. 결과가 없으면 빈 Region과 nil.
func (n *Naver) Reverse(ctx context.Context, lat, lng float64) (Region, error) {
	q := url.Values{}
	q.Set("coords", strconv.FormatFloat(lng, 'f', -1, 64)+","+strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("output", "json")
	q.Set("orders", "legalcode,admcode")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return Region{}, err
	}
	req.Header.Set("x-ncp-apigw-api-key-id", n.ClientID)
	req.Header.Set("x-ncp-apigw-api-key", n.ClientSecret)
	req.Header.Set("Accept", "application/json")

	hc := n.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: NaverTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		// url.Error는 요청 URL(좌표 포함)을 메시지에 넣으므로 원인만 남긴다.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Region{}, fmt.Errorf("naver reverse geocode: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Region{}, fmt.Errorf("naver reverse geocode: read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return Region{}, fmt.Errorf("naver reverse geocode: http %d", res.StatusCode)
	}

	var nr naverResponse
	if err := json.Unmarshal(body, &nr); err != nil {
		return Region{}, fmt.Errorf("naver reverse geocode: decode: %w", err)
	}
	switch nr.Status.Code {
	case naverStatusOK:
	case naverStatusNoResult:
		return Region{}, nil
	default:
		return Region{}, fmt.Errorf("naver reverse geocode: status %d %s", nr.Status.Code, nr.Status.Name)
	}
	if len(nr.Results) == 0 {
		return Region{}, nil
	}
	a := nr.Results[0].Region
	return Region{Area1: a.Area1.Name, Area2: a.Area2.Name, Area3: a.Area3.Name}, nil
}
