package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// loadDotEnv는 로컬 개발용으로 path(.env)의 KEY=VALUE를 환경 변수로 올린다.
// 이미 설정된 변수는 덮어쓰지 않고, 파일이 없으면 아무것도 하지 않는다.
// 값은 절대 로그에 남기지 않는다. 운영은 Secret Manager → 환경 변수.
func loadDotEnv(path string) (loaded int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return loaded, err
		}
		loaded++
	}
	return loaded, sc.Err()
}
