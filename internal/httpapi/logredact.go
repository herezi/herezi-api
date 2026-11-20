package httpapi

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// 로그 규칙(AGENTS.md, https://herezi.github.io/privacy/): 원좌표는 로그에 남기지 않는다.
// 접속 로그의 쿼리에서 좌표 파라미터는 소수 둘째 자리(약 1km)로 줄이고,
// 키·비밀값처럼 보이는 파라미터는 값을 지운다. 나머지 파라미터는 그대로 둔다.

// coordParams는 로그에서 소수 둘째 자리로 줄이는 좌표 파라미터.
var coordParams = map[string]bool{
	"lat": true, "lng": true,
	"sw_lat": true, "sw_lng": true,
	"ne_lat": true, "ne_lng": true,
}

const redacted = "REDACTED"

func isSecretParam(name string) bool {
	n := strings.ToLower(name)
	for _, s := range []string{"key", "secret", "token", "password", "auth"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// coarseCoord는 좌표 문자열을 소수 둘째 자리로 줄인다. 숫자가 아니면 값을 지운다.
func coarseCoord(raw string) string {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return redacted
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// logQuery는 접속 로그에 남길 쿼리 문자열(키 정렬)을 만든다.
func logQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	out := make(url.Values, len(q))
	for k, vs := range q {
		vv := make([]string, len(vs))
		for i, v := range vs {
			switch {
			case coordParams[k]:
				vv[i] = coarseCoord(v)
			case isSecretParam(k):
				vv[i] = redacted
			default:
				vv[i] = v
			}
		}
		out[k] = vv
	}
	return out.Encode()
}

// logErr는 에러 로그용 문자열. url.Error라면 URL(쿼리에 좌표가 있을 수 있음)을 빼고 원인만 남긴다.
func logErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}
