package httpapi

import (
	"net/http"

	"github.com/herezi/herezi-api/internal/geo"
)

// geoReverseJSON is components.schemas.GeoReverse.
type geoReverseJSON struct {
	Area1 string `json:"area1"`
	Area2 string `json:"area2"`
	Area3 string `json:"area3"`
	Label string `json:"label"`
}

// WithGeocoder sets the reverse geocoder for /v1/geo/reverse. 없으면 501.
func WithGeocoder(g geo.ReverseGeocoder) Option { return func(s *Server) { s.geocoder = g } }

// reverseGeocode는 좌표를 행정구역 이름으로 바꾼다. 네이버 키가 서버에만 있으므로 앱 대신 부른다.
// 결과가 없는 좌표(바다 등)는 200 + 빈 문자열. 외부 실패는 502 — 앱은 OS 지오코더 결과를 쓴다.
func (s *Server) reverseGeocode(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	lat, err := parseCoord(v.Get("lat"), "lat", 90)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	lng, err := parseCoord(v.Get("lng"), "lng", 180)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, err.Error())
		return
	}
	if s.geocoder == nil {
		writeError(w, http.StatusNotImplemented, codeNotConfigured, "reverse geocoding is not configured")
		return
	}
	region, err := s.geocoder.Reverse(r.Context(), lat, lng)
	if err != nil {
		s.log.Warn("reverse geocode failed", "err", err)
		writeError(w, http.StatusBadGateway, codeUpstream, "reverse geocoding failed")
		return
	}
	writeJSON(w, http.StatusOK, geoReverseJSON{
		Area1: region.Area1,
		Area2: region.Area2,
		Area3: region.Area3,
		Label: region.Label(),
	})
}
