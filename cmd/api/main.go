// Command api serves the public HTTP API (api/openapi.yaml).
//
// 환경 변수:
//   - PORT: 기본 8080
//   - DATABASE_URL: PostgreSQL(PostGIS) 접속 문자열. 없으면 메모리 저장소로 뜬다.
//   - MIGRATE_ON_START: "1"이면 시작할 때 goose 마이그레이션(migrations/)을 최신까지 적용한다(DATABASE_URL 필요).
//   - SNAPSHOT_PATH: DATABASE_URL이 없을 때 메모리 저장소에 올릴 수집 스냅샷(cmd/ingest -out).
//     없으면 예시 데이터(seed).
//   - NCP_MAPS_CLIENT_ID / NCP_MAPS_CLIENT_SECRET: 네이버 클라우드 Maps 키(역지오코딩).
//     없으면 /v1/geo/reverse는 501.
//   - SUPABASE_URL: Supabase 프로젝트 URL(https://<ref>.supabase.co). 로그인 토큰(JWT)을 이 프로젝트의
//     JWKS로 검증한다(iss = SUPABASE_URL/auth/v1). 없으면 /v1/me/*는 503 auth_not_configured.
//   - SUPABASE_JWT_SECRET: (선택) 레거시 HS256 JWT 비밀값. 있으면 HS256 토큰도 받는다.
//   - SUPABASE_SERVICE_ROLE_KEY: 서비스 역할 키(회원 탈퇴 때 Supabase Admin API로 사용자 삭제).
//     없으면 DELETE /v1/me는 503 auth_not_configured.
//
// 비밀값은 로그에 남기지 않는다(설정 여부만).
//
// 로컬 개발: 작업 디렉터리의 .env(gitignore)가 있으면 아직 설정되지 않은 변수만 읽어 온다.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/herezi/herezi-api/internal/auth"
	"github.com/herezi/herezi-api/internal/config"
	"github.com/herezi/herezi-api/internal/geo"
	"github.com/herezi/herezi-api/internal/httpapi"
	"github.com/herezi/herezi-api/internal/snapshot"
	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/postgres"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if n, err := config.LoadDotEnv(".env"); err != nil {
		return err
	} else if n > 0 {
		log.Info("loaded .env", "vars", n) // 값은 남기지 않는다
	}

	var spots store.SpotReader
	var saved store.SavedStore
	var notices store.NoticeReader
	if url := os.Getenv("DATABASE_URL"); url != "" {
		if os.Getenv("MIGRATE_ON_START") == "1" {
			n, err := postgres.Migrate(ctx, url)
			if err != nil {
				return err
			}
			log.Info("migrations applied", "count", n)
		}
		pg, err := postgres.Open(ctx, url)
		if err != nil {
			return err
		}
		defer func() { _ = pg.Close() }()
		spots, saved, notices = pg, pg, pg
		log.Info("store: postgres")
	} else if path := os.Getenv("SNAPSHOT_PATH"); path != "" {
		snap, generatedAt, err := snapshot.Read(path)
		if err != nil {
			return err
		}
		mem := memory.New(snap...)
		_, _ = mem.InsertNotice(ctx, memory.SeedNotice())
		spots, saved, notices = mem, mem, mem
		log.Info("store: memory from snapshot", "spots", len(snap), "generated_at", generatedAt)
	} else {
		mem := memory.New(memory.SeedSeoul(time.Now())...)
		_, _ = mem.InsertNotice(ctx, memory.SeedNotice())
		spots, saved, notices = mem, mem, mem
		log.Warn("store: memory with seed data (DATABASE_URL and SNAPSHOT_PATH not set)")
	}

	var opts []httpapi.Option
	if id, secret := os.Getenv("NCP_MAPS_CLIENT_ID"), os.Getenv("NCP_MAPS_CLIENT_SECRET"); id != "" && secret != "" {
		opts = append(opts, httpapi.WithGeocoder(geo.NewCache(geo.NewNaver(id, secret), geo.DefaultCacheTTL)))
		log.Info("geocoder: naver")
	} else {
		log.Warn("geocoder: disabled (NCP_MAPS_CLIENT_ID/SECRET not set), /v1/geo/reverse returns 501")
	}

	opts = append(opts, httpapi.WithSaved(saved), httpapi.WithNotices(notices))
	if u := os.Getenv("SUPABASE_URL"); u != "" {
		secret := os.Getenv("SUPABASE_JWT_SECRET")
		opts = append(opts, httpapi.WithAuth(auth.NewSupabase(auth.SupabaseConfig{URL: u, JWTSecret: secret})))
		log.Info("auth: supabase", "hs256", secret != "")
		if key := os.Getenv("SUPABASE_SERVICE_ROLE_KEY"); key != "" {
			opts = append(opts, httpapi.WithUserDeleter(auth.NewSupabaseAdmin(u, key, nil)))
			log.Info("account deletion: supabase admin")
		} else {
			log.Warn("account deletion: disabled (SUPABASE_SERVICE_ROLE_KEY not set), DELETE /v1/me returns 503")
		}
	} else {
		log.Warn("auth: disabled (SUPABASE_URL not set), /v1/me/* returns 503")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(spots, log, opts...).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
