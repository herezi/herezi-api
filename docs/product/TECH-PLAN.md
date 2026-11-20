# Herezi 기술 기획서

| 항목 | 내용 |
|---|---|
| 상태 | **승인됨** (2026-11-20, 위임 승인) |
| 버전 | v1.1 (2026-11-20) |
| 근거 | [SERVICE-PLAN.md](SERVICE-PLAN.md) v1.0, [DATA-PLAN.md](DATA-PLAN.md) |
| 출시 목표 | 2027년 3월 말, 서울, iOS · Android 동시 |

> 에이전트 규칙: 기술 선택을 바꾸려면 이 문서를 먼저 고치고 PR로 남긴다. 큰 결정은 `docs/adr/`에 ADR로 기록한다.

---

## 1. 전체 구조

```
 공공데이터 API ─┐
 (TourAPI, KOPIS, │   ┌────────────┐   ┌──────────────────┐   ┌──────────┐
  서울 열린데이터…) ├──▶│ ingest 작업 │──▶│ PostgreSQL+PostGIS │◀──│ api 서버  │◀── iOS / Android 앱
 OpenAI GPT ──────┘   │ (주기 실행) │   └──────────────────┘   │ (Go)     │
 네이버 지오코딩 ─────▶└────────────┘                          └──────────┘
```

- **api**: 읽기 전용 REST. 앱은 위치·필터로 근처 항목을 조회만 한다. 가입·쓰기 없음(M1).
- **ingest**: 소스별 수집기 → 정규화 → 지오코딩 → GPT 보강(요약·분류) → 중복 제거 → 저장. 주기 실행 배치.
- **계약**: `api/openapi.yaml`이 단일 기준. 앱 레포는 `make sync-api`로 복사해서 사용.

## 2. 레포와 스택

| 레포 | 스택 | 비고 |
|---|---|---|
| `herezi-api` | Go 1.25, 표준 `net/http`, GORM(pgx 드라이버), goose, PostGIS | `cmd/api`, `cmd/ingest` 두 실행 파일 |
| `herezi-ios` | Swift 6, SwiftUI, iOS 17+, XcodeGen, 네이버 지도 SDK(SPM) | `.xcodeproj`는 생성물, 커밋 안 함 |
| `herezi-android` | Kotlin, Jetpack Compose, 멀티모듈, Hilt, Retrofit + kotlinx.serialization, 네이버 지도 SDK | minSdk 26 |

### 결정과 이유
| 결정 | 이유 |
|---|---|
| Go 표준 라이브러리 위주 | 엔드포인트가 적고 단순. 의존성 최소화가 에이전트 개발에 유리 |
| PostGIS `geography` + GiST 인덱스 | 반경 검색·거리순 정렬을 DB에서 정확하게 처리 |
| GORM | 사용자 결정(2026-11-20). PostGIS 공간 쿼리는 GORM raw 표현식으로 작성, 스키마는 goose SQL 마이그레이션이 기준 |
| 네이버 지도 (양 앱) | 국내 지도 품질, 길찾기를 네이버지도 앱으로 연결(기획 P0) |
| 지오코딩: 네이버 클라우드 Geocoding | 좌표 없는 소스 항목 보완. 실패 시 GPT로 주소 정리 후 재시도 |
| LLM: OpenAI GPT | 요약·카테고리 분류·무료 여부 추출. 결과는 DB에 캐시, 원문 해시가 같으면 재호출 안 함 |
| 앱 아키텍처: 기능별 모듈 + 도메인 포트 | 가짜 서비스(Fake)로 서버 없이 화면 개발·테스트 가능 |

## 3. 데이터 모델 (요약)

`spots` 테이블 (`migrations/`):
- `id = "<source>:<source_id>"`, `source`, `source_id`
- `title`, `summary`, `category`(9종, `free` 제외), `is_free`
- `geog geography(Point,4326)`, `address`, `place_name`
- `starts_at`, `ends_at`, `image_url`, `source_url`
- `dedup_key`: 정규화 제목 + 기간 + 반경 100m 묶음으로 소스 간 중복 제거
- `content_hash`, `enriched_at`: GPT 재호출 방지
- `fetched_at`: 앱에 "갱신 시각"으로 표시

조회 규칙: `ends_at < now()`인 항목은 반환하지 않는다(끝난 행사 숨김).

## 4. API (M1)

| 메서드 | 경로 | 설명 |
|---|---|---|
| GET | `/v1/spots` | `lat`, `lng`, `radius_m`, `category`, `when`, `free`, `cursor`, `limit` → 거리순 |
| GET | `/v1/spots/{id}` | 상세 |
| GET | `/v1/spots/map` | 지도 영역(bbox) 조회, 클러스터는 앱에서 처리 *(계약에 추가 예정)* |

- `Spot`에 `fetched_at`(갱신 시각) 추가 예정 — 상세 화면 요구사항. 계약 변경은 M0 이슈에서.
| GET | `/healthz` | 헬스체크 |

- 인증 없음. 대신 앱별 API 키 헤더 + IP 기준 속도 제한.
- 응답 캐시: `Cache-Control: max-age=60`.

## 5. 인프라

| 항목 | 선택 | 이유 |
|---|---|---|
| 서버 | Google Cloud Run (서울 `asia-northeast3`) | 컨테이너, 사용량 과금, 0대까지 축소 |
| 배치 | Cloud Run Jobs + Cloud Scheduler | `cmd/ingest`를 소스별 주기로 실행 |
| DB | Supabase Postgres (서울 리전, PostGIS) | 관리형, 무료~저가 시작 |
| 비밀값 | Secret Manager | API 키(TourAPI, KOPIS, 서울, 네이버, OpenAI) |
| 이미지 | 원본 URL 사용(M1) | 저장·가공은 M2에서 필요 시 |
| 모니터링 | Cloud Logging + 수집 결과 요약 로그, 실패 알림 | 수집 0건·급감 시 알림 |

> 비용·계정 결정은 M0에서 실제 생성하며 확정한다. 바뀌면 이 표를 고친다.

## 6. 개발 방식 (하네스 엔지니어링)

- 레포마다 `AGENTS.md`(규칙·명령·구조), `docs/`(기획·ADR·실행 계획), `make check`(포맷·린트·테스트) 제공.
- GitHub Actions CI: PR마다 `make check`. 통과해야 머지.
- 모든 작업: 이슈 → 브랜치 → PR(`Closes #n`) → 머지. main 직접 푸시 금지.
- 테스트: 도메인 단위 테스트, API는 DB 없는 메모리 저장소로 핸들러 테스트 + PostGIS 통합 테스트(CI 서비스 컨테이너), 앱은 Fake 서비스로 뷰모델 테스트.

## 7. 일정 (마일스톤)

| 마일스톤 | 기간 | 산출물 |
|---|---|---|
| M0 기반 | 2026-11 말 ~ 12월 | 하네스(AGENTS.md·CI), API 키 발급, 데이터 실측, DB·배포 환경 |
| M1-a 수집 | 12월 ~ 2027-01 | 소스별 수집기, 지오코딩, GPT 보강, 중복 제거 |
| M1-b API | 12월 ~ 2027-01 | `/v1/spots` 실데이터 연결, 배포 |
| M1-c 앱 | 2027-01 ~ 02 | 근처 목록·지도·필터·상세·길찾기, 위치 미허용 대응, 공유·찜·검색(P1) |
| 베타 | 2027-03 초 | TestFlight · 내부 테스트 트랙, 봄꽃 데이터 점검 |
| 출시 | 2027-03 말 | 스토어 출시 |

## 8. 기존 초안 코드 처리

승인 전에 만들어진 초안 코드(커밋 안 됨)가 세 레포에 있다.
- `herezi-api`: 빌드·테스트 통과. **M0 이슈에서 검토 후 PR로 반영**(`free` 카테고리 제거 등 v1.0 기획에 맞춰 수정).
- `herezi-ios`: 구조는 이 문서와 일치. M1-c에서 검토 후 반영.
- `herezi-android`: 빌드 파일만 있고 소스가 없음. M1-c에서 이 문서 기준으로 새로 작성.

## 변경 이력
| 날짜 | 버전 | 내용 |
|---|---|---|
| 2026-11-20 | v1.0 | 작성 및 승인 |
| 2026-11-20 | v1.1 | DB 접근을 sqlc → GORM으로 변경 ([ADR 0002](../adr/0002-gorm.md)) |
