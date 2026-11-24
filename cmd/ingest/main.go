// Command ingest는 공공데이터 수집 배치다(DATA-PLAN.md §6, 이슈 #42).
//
//	수집(소스별) → 정규화 → 필터(끝남·서울 밖) → 좌표 보강(장소명 색인·네이버 Geocoding)
//	→ 중복 제거 → GPT 보강(요약·애매한 분류·애매한 무료, 해시 캐시·호출 상한) → 저장
//
// 저장: DATABASE_URL이 있으면 PostGIS에 upsert(GORM), 없으면 -out JSON 스냅샷.
// cmd/api는 DATABASE_URL 없이 SNAPSHOT_PATH로 스냅샷을 메모리 저장소에 올린다.
//
// 환경 변수(로컬은 .env): SEOUL_OPENAPI_KEY, DATA_GO_KR_SERVICE_KEY, OPENAI_API_KEY,
// NCP_MAPS_CLIENT_ID/NCP_MAPS_CLIENT_SECRET, DATABASE_URL(선택). 값은 출력하지 않는다.
//
// 예: go run ./cmd/ingest -out data/snapshot.json -max-gpt 150
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/herezi/herezi-api/internal/config"
	"github.com/herezi/herezi-api/internal/domain"
	"github.com/herezi/herezi-api/internal/geo"
	"github.com/herezi/herezi-api/internal/ingest"
	"github.com/herezi/herezi-api/internal/ingest/openai"
	"github.com/herezi/herezi-api/internal/ingest/sources"
	"github.com/herezi/herezi-api/internal/snapshot"
	"github.com/herezi/herezi-api/internal/store/postgres"
)

const allSources = "seoul_culture,seoul_reservation,tourapi,cultureinfo"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("ingest failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	srcFlag := flag.String("sources", allSources, "수집할 소스(쉼표 구분)")
	maxGPT := flag.Int("max-gpt", 300, "GPT 요청 수 상한(배치 하나 = 1회). 0이면 GPT 안 씀")
	batch := flag.Int("batch", 10, "GPT 요청 하나에 넣을 항목 수")
	out := flag.String("out", "data/snapshot.json", "DATABASE_URL이 없을 때 쓸 스냅샷 경로(이전 스냅샷은 해시 캐시로 읽는다)")
	dryRun := flag.Bool("dry-run", false, "저장하지 않고 리포트만 출력")
	horizon := flag.Int("horizon-days", 90, "시작이 이보다 먼 미래인 항목 제외(0 = 제한 없음)")
	model := flag.String("model", openai.DefaultModel, "OpenAI 모델")
	priceIn := flag.Float64("price-in", 0.20, "비용 추정용 입력 토큰 단가(USD/1M)")
	priceOut := flag.Float64("price-out", 1.25, "비용 추정용 출력 토큰 단가(USD/1M)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	srcs, err := buildSources(*srcFlag)
	if err != nil {
		return err
	}
	p := &ingest.Pipeline{
		Sources: srcs, MaxGPTCalls: *maxGPT, BatchSize: *batch, HorizonDays: *horizon, Log: log,
	}
	if id, secret := os.Getenv("NCP_MAPS_CLIENT_ID"), os.Getenv("NCP_MAPS_CLIENT_SECRET"); id != "" && secret != "" {
		p.Geocoder = geo.NewNaverGeocoder(id, secret)
	} else {
		log.Warn("geocoder disabled (NCP_MAPS_CLIENT_ID/SECRET not set)")
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && *maxGPT > 0 {
		c := openai.New(key)
		c.Model = *model
		p.Enricher = c
	} else {
		log.Warn("gpt disabled (OPENAI_API_KEY not set or -max-gpt 0)")
	}

	dbURL := os.Getenv("DATABASE_URL")
	var pg *postgres.Store
	var prior []domain.Spot
	if dbURL != "" {
		if pg, err = postgres.Open(ctx, dbURL); err != nil {
			return err
		}
		defer func() { _ = pg.Close() }()
		if prior, err = pg.EnrichedSpots(ctx); err != nil {
			return err
		}
	} else if prior, _, err = snapshot.Read(*out); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("previous snapshot unreadable, starting without cache", "err", err)
	}
	p.Prior = map[string]domain.Spot{}
	for _, sp := range prior {
		if sp.ContentHash != "" && sp.Summary != "" {
			p.Prior[sp.ContentHash] = sp
		}
	}

	started := time.Now()
	res := p.Run(ctx)
	fmt.Print(res.Report.String())
	cost := float64(res.Report.PromptTokens)/1e6**priceIn + float64(res.Report.CompletionTokens)/1e6**priceOut
	fmt.Printf("cache entries=%d  est_gpt_cost=$%.4f (model %s, 단가 가정 in $%.2f/out $%.2f per 1M)  elapsed=%s\n",
		len(p.Prior), cost, *model, *priceIn, *priceOut, time.Since(started).Round(time.Second))

	switch {
	case *dryRun:
		fmt.Println("dry-run: 저장하지 않음")
	case pg != nil:
		for _, sp := range res.Spots {
			if err := pg.UpsertSpot(ctx, sp); err != nil {
				return err
			}
		}
		fmt.Printf("saved %d spots to postgres\n", len(res.Spots))
	default:
		if err := snapshot.Write(*out, time.Now().UTC(), res.Spots); err != nil {
			return err
		}
		fmt.Printf("saved %d spots to %s\n", len(res.Spots), *out)
	}
	return nil
}

func buildSources(list string) ([]ingest.Source, error) {
	seoulKey, dataKey := os.Getenv("SEOUL_OPENAPI_KEY"), os.Getenv("DATA_GO_KR_SERVICE_KEY")
	var out []ingest.Source
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		need := func(key, env string) error {
			if key == "" {
				return fmt.Errorf("source %s needs %s", name, env)
			}
			return nil
		}
		switch name {
		case "":
			continue
		case "seoul_culture":
			if err := need(seoulKey, "SEOUL_OPENAPI_KEY"); err != nil {
				return nil, err
			}
			out = append(out, sources.NewSeoulCulture(seoulKey))
		case "seoul_reservation":
			if err := need(seoulKey, "SEOUL_OPENAPI_KEY"); err != nil {
				return nil, err
			}
			out = append(out, sources.NewSeoulReservation(seoulKey))
		case "tourapi":
			if err := need(dataKey, "DATA_GO_KR_SERVICE_KEY"); err != nil {
				return nil, err
			}
			out = append(out, sources.NewTourAPI(dataKey))
		case "cultureinfo":
			if err := need(dataKey, "DATA_GO_KR_SERVICE_KEY"); err != nil {
				return nil, err
			}
			out = append(out, sources.NewCultureInfo(dataKey))
		default:
			return nil, fmt.Errorf("unknown source %q (known: %s)", name, allSources)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no sources")
	}
	return out, nil
}
