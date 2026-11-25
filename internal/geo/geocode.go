// Package geo는 수집 배치(cmd/ingest)가 쓰는 지오코딩(주소 → 좌표, 네이버)과 장소 검색(카카오)을 다룬다.
// 외부 서비스는 인터페이스(Geocoder, PlaceSearcher) 뒤에 숨겨 파이프라인은 가짜 구현으로 테스트한다.
// 역지오코딩(/v1/geo/reverse)은 #74로 제거했다 — 앱은 기기 지오코더를 쓴다.
// domain 외의 internal 패키지를 import하지 않는다.
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
	"strings"
	"time"
)

// NaverGeocodeURL은 네이버 클라우드 Maps Geocoding(주소 → 좌표) 엔드포인트.
const NaverGeocodeURL = "https://maps.apigw.ntruss.com/map-geocode/v2/geocode"

// NaverGeocodeTimeout은 배치(수집)용 제한 시간.
const NaverGeocodeTimeout = 5 * time.Second

// Point는 지오코딩 결과 좌표. 결과가 없으면 OK가 false.
type Point struct {
	Lat, Lng float64
	OK       bool
}

// Geocoder는 주소 문자열을 좌표로 바꾼다. 네이버 Geocoding은 주소만 찾고 장소명(POI)은 못 찾는다.
type Geocoder interface {
	Geocode(ctx context.Context, query string) (Point, error)
}

// NaverGeocoder는 네이버 클라우드 Maps Geocoding 클라이언트(서버 전용, 수집 배치에서 사용).
type NaverGeocoder struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
}

// NewNaverGeocoder는 기본 엔드포인트·5초 타임아웃으로 클라이언트를 만든다.
func NewNaverGeocoder(clientID, clientSecret string) *NaverGeocoder {
	return &NaverGeocoder{
		BaseURL: NaverGeocodeURL, ClientID: clientID, ClientSecret: clientSecret,
		HTTP: &http.Client{Timeout: NaverGeocodeTimeout},
	}
}

type naverGeocodeResponse struct {
	Status       string `json:"status"`
	ErrorMessage string `json:"errorMessage"`
	Addresses    []struct {
		X string `json:"x"` // 경도
		Y string `json:"y"` // 위도
	} `json:"addresses"`
}

// Geocode는 query의 첫 번째 결과 좌표를 돌려준다. 결과가 없으면 Point{OK:false}와 nil.
func (n *NaverGeocoder) Geocode(ctx context.Context, query string) (Point, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Point{}, nil
	}
	q := url.Values{}
	q.Set("query", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return Point{}, err
	}
	req.Header.Set("x-ncp-apigw-api-key-id", n.ClientID)
	req.Header.Set("x-ncp-apigw-api-key", n.ClientSecret)
	req.Header.Set("Accept", "application/json")

	hc := n.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: NaverGeocodeTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		var ue *url.Error // URL(주소 질의)을 메시지에 넣지 않는다
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Point{}, fmt.Errorf("naver geocode: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Point{}, fmt.Errorf("naver geocode: read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return Point{}, fmt.Errorf("naver geocode: http %d", res.StatusCode)
	}
	var nr naverGeocodeResponse
	if err := json.Unmarshal(body, &nr); err != nil {
		return Point{}, fmt.Errorf("naver geocode: decode: %w", err)
	}
	if nr.Status != "OK" {
		return Point{}, fmt.Errorf("naver geocode: status %s", nr.Status)
	}
	if len(nr.Addresses) == 0 {
		return Point{}, nil
	}
	lng, err1 := strconv.ParseFloat(nr.Addresses[0].X, 64)
	lat, err2 := strconv.ParseFloat(nr.Addresses[0].Y, 64)
	if err1 != nil || err2 != nil {
		return Point{}, fmt.Errorf("naver geocode: bad coordinates")
	}
	return Point{Lat: lat, Lng: lng, OK: true}, nil
}
