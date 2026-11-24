# ADR 0003: 배포를 GCP에서 Railway + Supabase로

| 항목 | 내용 |
|---|---|
| 상태 | 승인됨 (사용자 결정, 2026-11-24) |
| 날짜 | 2026-11-24 |
| 대체 | [TECH-PLAN.md](../product/TECH-PLAN.md) v1.2 §5의 Cloud Run·Cloud Scheduler·Secret Manager |
| 근거 | 이슈 #7, TECH-PLAN v1.4 §5 |

## 맥락
- M1은 트래픽이 거의 없고 운영 인원이 한 명이다. GCP 프로젝트·결제·IAM을 새로 만드는 비용이 크다.
- 사용자는 이미 Railway Hobby 계정(다른 프로젝트 "언제볼까")을 쓰고 있다.

## 결정
- **API·수집기: Railway** 새 프로젝트 `herezi`. 같은 Dockerfile로 서비스 두 개 — `api`(상시, `/healthz`), `ingest`(cron `0 20 * * *` UTC = 05:00 KST). 리전은 Singapore.
- **DB: Supabase 무료**, 별도 조직 `herezi` / 프로젝트 `herezi`, `ap-southeast-1`. PostGIS·pg_trgm. 접속은 session pooler(IPv4).
- Supabase Data API로 테이블이 노출되지 않도록 RLS를 켜고 `anon`·`authenticated` 권한을 회수한다(마이그레이션 00005). 앱은 DB에 직접 붙지 않는다.
- 비밀값은 Railway 서비스 변수.

## 대안
- **Cloud Run + Cloud SQL/Supabase(서울)**: 서울 리전으로 지연은 가장 짧지만 새 GCP 설정·결제가 필요하다.
- **Fly.io**: 서울/도쿄 리전이 있으나 새 계정·결제.

## 결과
- 좋음: 기존 결제 재사용, 설정 최소, 월 추가 비용 약 US$2~5.
- 비용: 싱가포르라 한국 사용자 왕복 지연이 약 70~90ms 늘어난다(목록 조회 1회라 감당 가능). Supabase 무료는 7일 비활성 시 일시정지 → 매일 수집 cron으로 방지. 트래픽이 늘면 Supabase Pro·서울/도쿄 리전 이전을 다시 검토한다.
