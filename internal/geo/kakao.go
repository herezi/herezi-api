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

// KakaoKeywordURL은 카카오 로컬 키워드 장소 검색 엔드포인트.
const KakaoKeywordURL = "https://dapi.kakao.com/v2/local/search/keyword.json"

// KakaoTimeout은 배치(수집)용 제한 시간.
const KakaoTimeout = 5 * time.Second

// SeoulRect는 카카오 rect 파라미터용 서울 범위(좌하단 경도,위도,우상단 경도,위도).
const SeoulRect = "126.734,37.413,127.270,37.716"

// Place는 장소 검색 결과 하나.
type Place struct {
	Name    string
	Address string
	Lat     float64
	Lng     float64
}

// PlaceSearcher는 장소명(POI)으로 후보 장소를 찾는다. 네이버 Geocoding이 못 찾는 장소명 보완용.
type PlaceSearcher interface {
	SearchPlace(ctx context.Context, query string) ([]Place, error)
}

// KakaoPlaces는 카카오 로컬 키워드 검색 클라이언트(REST API 키, 서버 전용).
type KakaoPlaces struct {
	BaseURL string
	APIKey  string
	Rect    string // 비어 있으면 범위 제한 없음
	Size    int    // 결과 수(1~15)
	HTTP    *http.Client
}

// NewKakaoPlaces는 기본 엔드포인트·서울 범위·5초 타임아웃으로 클라이언트를 만든다.
func NewKakaoPlaces(apiKey string) *KakaoPlaces {
	return &KakaoPlaces{
		BaseURL: KakaoKeywordURL, APIKey: apiKey, Rect: SeoulRect, Size: 5,
		HTTP: &http.Client{Timeout: KakaoTimeout},
	}
}

type kakaoKeywordResponse struct {
	Documents []struct {
		PlaceName       string `json:"place_name"`
		AddressName     string `json:"address_name"`
		RoadAddressName string `json:"road_address_name"`
		X               string `json:"x"` // 경도
		Y               string `json:"y"` // 위도
	} `json:"documents"`
}

// SearchPlace는 query로 장소를 검색해 정확도순 후보를 돌려준다. 결과가 없으면 빈 슬라이스와 nil.
func (k *KakaoPlaces) SearchPlace(ctx context.Context, query string) ([]Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("query", query)
	if k.Rect != "" {
		q.Set("rect", k.Rect)
	}
	if k.Size > 0 {
		q.Set("size", strconv.Itoa(k.Size))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "KakaoAK "+k.APIKey)
	req.Header.Set("Accept", "application/json")

	hc := k.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: KakaoTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		var ue *url.Error // URL(질의)을 메시지에 넣지 않는다
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("kakao keyword: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("kakao keyword: read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kakao keyword: http %d", res.StatusCode)
	}
	var kr kakaoKeywordResponse
	if err := json.Unmarshal(body, &kr); err != nil {
		return nil, fmt.Errorf("kakao keyword: decode: %w", err)
	}
	out := make([]Place, 0, len(kr.Documents))
	for _, d := range kr.Documents {
		lng, err1 := strconv.ParseFloat(d.X, 64)
		lat, err2 := strconv.ParseFloat(d.Y, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		addr := d.RoadAddressName
		if addr == "" {
			addr = d.AddressName
		}
		out = append(out, Place{Name: d.PlaceName, Address: addr, Lat: lat, Lng: lng})
	}
	return out, nil
}
