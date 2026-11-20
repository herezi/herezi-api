# ADR 0001: 서버 스택 — Go 표준 net/http, PostgreSQL+PostGIS, pgx, sqlc, goose

| 항목 | 내용 |
|---|---|
| 상태 | 승인됨 |
| 날짜 | 2026-11-20 |
| 근거 | [TECH-PLAN.md](../product/TECH-PLAN.md) §2 (v1.0 승인) |

## 맥락
- API는 읽기 전용 엔드포인트 몇 개(`/v1/spots`, `/v1/spots/{id}`, 지도 bbox 조회, 헬스체크)뿐이다.
- 핵심 질의는 "이 좌표에서 반경 N m 안, 지금 진행 중인 것, 거리순"이다. 정확한 거리 계산과 공간 인덱스가 필요하다.
- 코드는 주로 에이전트가 작성·수정한다. 의존성과 마법(리플렉션·코드 생성 프레임워크)이 적을수록 실수가 줄고 리뷰가 쉽다.
- 수집 배치(`cmd/ingest`)와 API(`cmd/api`)가 같은 도메인 타입과 저장소를 공유한다.

## 결정
| 영역 | 선택 |
|---|---|
| 언어 | Go (go.mod 버전 고정) |
| HTTP | 표준 `net/http` (`ServeMux` 메서드·경로 패턴). 웹 프레임워크 없음 |
| DB | PostgreSQL + PostGIS, `geography(Point,4326)` + GiST 인덱스 |
| 드라이버 | pgx v5 (`pgxpool`) |
| 쿼리 | sqlc: `queries.sql` → 타입 안전 Go 코드(`sqlcdb/`, 생성물 커밋) |
| 마이그레이션 | goose (SQL 파일, `embed`로 바이너리·테스트에 포함) |
| 로깅 | 표준 `log/slog` (JSON) |
| 테스트 | 표준 `testing` + `httptest`, 메모리 저장소, 저장소 공통 스위트, PostGIS 통합 테스트(빌드 태그) |
| 린트 | gofmt, go vet, staticcheck(버전 고정) — `make check` |

## 대안
- **웹 프레임워크(Gin, Echo, chi)**: 엔드포인트가 적고 Go 1.22+ `ServeMux`로 충분. 의존성만 는다.
- **ORM(GORM, ent)**: 공간 질의는 어차피 생 SQL이 필요하고, 생성되는 쿼리를 예측하기 어렵다.
- **애플리케이션에서 거리 계산(위경도 컬럼 + 하버사인)**: 데이터가 늘면 인덱스 없이 전 범위 스캔. PostGIS가 정확·빠름.
- **관리형 검색(Elasticsearch, Algolia 지오 검색)**: 비용·운영 부담 대비 M1 규모에 과함.

## 결과
- 좋음: 의존성이 적고(pgx, goose, yaml 테스트용), SQL이 그대로 보이며, 메모리 저장소로 DB 없이 핸들러를 테스트할 수 있다.
- 비용: sqlc CLI가 있어야 쿼리를 바꿀 수 있다(`brew install sqlc`). 메모리 저장소가 PostGIS 의미를 흉내내므로 둘이 어긋날 수 있다 → `storetest` 공통 스위트를 양쪽에서 돌려 막는다.
- 로컬에 PostGIS가 없으면 통합 테스트는 CI(서비스 컨테이너)에서만 돈다.
