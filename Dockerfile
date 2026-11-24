# herezi-api 이미지: cmd/api(기본 실행)와 cmd/ingest(수집 cron)를 함께 담는다.
#   api:    /app/api                      ($PORT 리슨, MIGRATE_ON_START=1이면 시작 시 마이그레이션)
#   ingest: /app/ingest -max-gpt 100      (DATABASE_URL에 upsert 후 종료)
# 비밀값은 이미지에 넣지 않는다(.dockerignore로 .env 제외). 런타임 환경 변수로 받는다.

FROM golang:1.25-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags="-s -w" -o /out/ingest ./cmd/ingest \
 && mkdir -p /out/data

# 런타임은 alpine(셸 있음): Railway가 시작 명령을 셸로 실행해도 동작한다.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 app
WORKDIR /app
COPY --from=build /out/api /out/ingest /app/
# ingest의 -excluded-cache 기본 경로(data/excluded.json)를 쓸 수 있게 둔다(컨테이너마다 새로 시작).
COPY --from=build --chown=app:app /out/data /app/data
ENV PORT=8080
EXPOSE 8080
USER app
CMD ["/app/api"]
