# AGENTS.md — herezi-api

에이전트(그리고 사람)가 이 레포에서 작업하기 전에 읽는 문서. 짧게 유지하고, 자세한 내용은 링크로 보낸다.

## 이 레포는
Herezi("지금 내 주변에 뭐가 있는지") 서비스의 **Go 백엔드**.
- `cmd/api`: 읽기 전용 공개 REST API. 앱(iOS·Android)은 위치·필터로 근처 항목을 조회만 한다.
- `cmd/ingest`: 공공데이터 수집 배치(수집 → 정규화 → 지오코딩 → 중복 제거 → GPT 보강 → 저장). DB가 없으면 JSON 스냅샷(`data/snapshot.json`, gitignore).
- `api/openapi.yaml`: 서버·iOS·Android 사이 API 계약의 **단일 기준**.

## 먼저 읽을 문서
| 문서 | 내용 |
|---|---|
| [docs/product/PRODUCT.md](docs/product/PRODUCT.md) | 제품 한 줄·원칙·카테고리 (세 레포 공통 기준) |
| [docs/product/SERVICE-PLAN.md](docs/product/SERVICE-PLAN.md) | 서비스 기획: 사용자·기능 우선순위·화면 |
| [docs/product/TECH-PLAN.md](docs/product/TECH-PLAN.md) | 기술 기획: 구조·스택·API·인프라·일정 |
| [docs/product/DATA-PLAN.md](docs/product/DATA-PLAN.md) | 데이터 기획: 소스·수집·정규화 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 패키지 계층과 의존 규칙 |
| [docs/adr/](docs/adr/) | 아키텍처 결정 기록(ADR) |
| [docs/design/DESIGN.md](docs/design/DESIGN.md) | 디자인 시스템·화면 스펙 (iOS·Android 공통) |
| [api/openapi.yaml](api/openapi.yaml) | API 계약 |

## 폴더 구조
```
api/openapi.yaml            API 계약 (단일 기준)
cmd/api/                    API 서버 진입점 (환경 변수 → 저장소 선택 → HTTP)
cmd/ingest/                 수집 배치 진입점 (-sources, -max-gpt, -out, -dry-run, -excluded-cache, -show-excluded)
internal/domain/            핵심 타입·규칙 (Spot, Category, When, 거리). 다른 internal 패키지 import 금지
internal/store/             저장소 인터페이스 (SpotReader/SpotWriter, NearbyQuery)
internal/store/memory/      메모리 저장소 (테스트, DATABASE_URL 없을 때) + 서울 예시 데이터
internal/store/postgres/    PostGIS 저장소 (GORM, 공간 조건은 raw 표현식)
internal/store/storetest/   모든 저장소가 통과해야 하는 공통 테스트 스위트
internal/httpapi/           HTTP 핸들러·라우트 테이블·계약 테스트
internal/geo/               역지오코딩·지오코딩 인터페이스, 네이버 클라이언트, 메모리 캐시
internal/ingest/            수집 파이프라인(필터·지오코딩·중복 제거·GPT 보강), sources/(소스별 수집기), openai/(GPT 배치)
internal/snapshot/          수집 결과 JSON 스냅샷 읽기·쓰기
internal/config/            .env 로더 (cmd 공용)
migrations/                 goose SQL 마이그레이션 (embed)
docs/product/               기획 문서
docs/design/                디자인 시스템·화면 스펙·목업 (두 앱 공통)
docs/adr/                   ADR (NNNN-제목.md)
```

## 명령
| 명령 | 설명 |
|---|---|
| `make check` | gofmt·go vet·staticcheck·go test. **PR 전에 반드시 통과** (CI도 같은 명령) |
| `make run` | API 서버 실행 (`PORT` 기본 8080, `DATABASE_URL` 없으면 메모리 저장소 — `SNAPSHOT_PATH`가 있으면 그 스냅샷, 없으면 예시 데이터. 로컬 `.env`가 있으면 읽음) |
| `make ingest` | 수집 배치 1회 (`go run ./cmd/ingest -out data/snapshot.json`). 키는 `.env`. GPT는 월 한도 공유라 `-max-gpt`로 요청 수를 제한 |
| `make test` | 단위·핸들러 테스트만 (DB 불필요) |
| `make test-integration` | PostGIS 통합 테스트. `HEREZI_TEST_DATABASE_URL`(버려도 되는 DB) 필요. CI에서는 서비스 컨테이너로 실행 |

로컬에 Docker·PostGIS가 없어도 `make check`는 돌아간다. 통합 테스트는 빌드 태그 `integration`으로 분리되어 있다.

## 규칙
1. **이슈 → 브랜치 → PR → 머지.** 모든 작업은 GitHub 이슈에서 시작하고, 브랜치(`feat/…`, `fix/…`, `chore/…`, `docs/…`)에서 PR을 만든다. PR 본문에 `Closes #n`.
2. **브랜치 전략:** `feat/*`·`fix/*`·`chore/*` → `develop`(기본 브랜치)으로 PR. `main`은 출시 전용이며 `develop` → `main` 릴리스 PR로만 바뀐다. **main·develop에 직접 푸시하지 않는다.** `make check`와 CI가 통과한 PR만 squash 머지한다.
3. **기획 먼저, 코드는 승인 후.** 새 기능·큰 변경은 관련 기획 문서(docs/product)가 승인된 뒤에 구현한다. 기획에 없는 동작이 필요하면 먼저 문서를 고치는 PR을 낸다.
4. **`api/openapi.yaml`이 계약의 단일 기준.** 엔드포인트·필드·enum을 바꾸면 openapi를 먼저(같은 PR에서) 고친다. 계약 테스트(`internal/httpapi/contract_test.go`)가 카테고리 enum·라우트·JSON 필드를 대조한다. 하위호환을 지킨다(필드 삭제·의미 변경 금지, 추가는 가능).
5. **큰 결정은 ADR로.** 스택·저장소·외부 서비스·계층 규칙을 바꾸는 결정은 `docs/adr/NNNN-제목.md`로 남기고 TECH-PLAN.md도 함께 고친다.
6. **계층 규칙을 지킨다.** [ARCHITECTURE.md](ARCHITECTURE.md) 참고. `domain`은 아무것도 import하지 않고, `cmd`만 구체 구현을 조립한다.
7. **조회 규칙.** 끝난 행사(`ends_at < now`)는 목록(`/v1/spots`)·지도(`/v1/spots/map`)에 넣지 않는다. 상세(`/v1/spots/{id}`)는 끝난 행사도 200으로 돌려주고 `is_ended: true`를 붙인다(요청 시각 기준 계산). 무료는 카테고리가 아니라 `free=true` 필터다.
8. **DB 접근은 GORM, 스키마는 goose 마이그레이션.** 테이블·컬럼·인덱스는 `migrations/`에 새 SQL 파일로만 바꾼다. **`AutoMigrate` 금지**(PostGIS geography·GiST 인덱스를 표현할 수 없다). GORM 모델은 `internal/store/postgres` 안에만 두고 `domain`은 gorm을 import하지 않는다. 공간 조건은 raw 표현식에 `?` 바인딩으로. 저장소 동작을 바꾸면 `storetest` 스위트에 케이스를 추가해 메모리·postgres가 같은 결과를 내는지 확인한다.
9. **로그 규칙(위치정보).** 원좌표는 로그에 남기지 않는다([개인정보 처리방침](https://herezi.github.io/privacy/)). 접속 로그 미들웨어(`internal/httpapi/logredact.go`)가 쿼리의 `lat`·`lng`·`sw_lat`·`sw_lng`·`ne_lat`·`ne_lng`를 소수 둘째 자리(약 1km)로 줄여 기록하고(`/v1/spots`, `/v1/spots/map`, `/v1/geo/reverse` 모두), 키·토큰·비밀값으로 보이는 파라미터는 값을 지운다. 에러 로그는 `logErr`로 남겨 요청 URL(좌표 포함)을 빼고, 외부 클라이언트(예: 네이버) 에러 메시지에도 URL을 넣지 않는다. 좌표를 받는 엔드포인트를 추가하면 `coordParams`와 `logredact_test.go`에 함께 추가한다.
10. **비밀값을 커밋하지 않는다.** API 키·DB 접속 문자열은 환경 변수(운영은 Railway 서비스 변수).
11. 문서·주석은 한국어, 코드 식별자는 영어.

## 에이전트 역할
역할별 지침은 [`agents/`](agents/README.md)에 있다(기획자·디자이너·리뷰어·QA·개발자). 어떤 LLM 도구든 역할을 맡으면 해당 파일(`agents/<역할>.md`)을 먼저 읽고 따른다. Claude Code는 `.claude/agents` 링크로 자동 인식한다.
