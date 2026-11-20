# Herezi API — 에이전트와 사람이 같은 명령을 쓴다. 자세한 규칙은 AGENTS.md.

STATICCHECK_VERSION := v0.7.0
# go.mod의 Go 버전을 그대로 쓴다(도구 때문에 다른 툴체인을 내려받지 않음).
export GOTOOLCHAIN := local

.PHONY: check fmt vet lint test test-integration run generate tidy

## check: 포맷·vet·staticcheck·테스트. PR 전에 반드시 통과해야 한다.
check: fmt vet lint test

## fmt: gofmt 검사 (고칠 파일이 있으면 실패)
fmt:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt 필요:"; echo "$$out"; exit 1; fi

## vet: go vet (통합 테스트 파일도 컴파일 확인)
vet:
	go vet ./...
	go vet -tags integration ./...

## lint: staticcheck (버전 고정)
lint:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) -tags integration ./...

## test: 단위·핸들러 테스트 (DB 불필요)
test:
	go test ./...

## test-integration: PostGIS 통합 테스트. HEREZI_TEST_DATABASE_URL 필요(버려도 되는 DB).
test-integration:
	@test -n "$$HEREZI_TEST_DATABASE_URL" || (echo "HEREZI_TEST_DATABASE_URL를 설정하세요"; exit 1)
	go test -tags integration -count=1 -v ./internal/store/postgres/...

## run: API 서버 실행. DATABASE_URL이 없으면 예시 데이터 메모리 저장소.
run:
	go run ./cmd/api

## generate: queries.sql → sqlcdb/ (sqlc 필요: brew install sqlc)
generate:
	sqlc generate

## tidy: go mod tidy
tidy:
	go mod tidy
