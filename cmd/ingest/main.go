// Command ingest는 공공데이터 수집 배치다(DATA-PLAN.md §6, 이슈 #42).
//
//	수집(소스별) → 정규화 → 필터(끝남·서울 밖) → 좌표 보강(장소명 색인·네이버 Geocoding·카카오 키워드)
//	→ 중복 제거 → GPT 보강(요약·애매한 분류·애매한 무료, 해시 캐시·호출 상한) → 저장
//
// 끝난 항목은 종료 후 -retain-ended-days(기본 90일) 동안 보존한다(상세·저장 목록용, 이슈 #51).
// 원천 목록에서 빠져도 지우지 않고(DB: last_seen_at만 갱신 중단, 스냅샷: 이전 스냅샷에서 유지),
// 보존 기간이 지난 항목과 볼거리 아님 판정 항목만 정리한다.
//
// 저장: DATABASE_URL이 있으면 PostGIS에 upsert(GORM), 없으면 -out JSON 스냅샷.
// cmd/api는 DATABASE_URL 없이 SNAPSHOT_PATH로 스냅샷을 메모리 저장소에 올린다.
//
// 환경 변수(로컬은 .env): SEOUL_OPENAPI_KEY, DATA_GO_KR_SERVICE_KEY, OPENAI_API_KEY,
// NCP_MAPS_CLIENT_ID/NCP_MAPS_CLIENT_SECRET, KAKAO_REST_API_KEY(선택), DATABASE_URL(선택). 값은 출력하지 않는다.
//
// GPT가 볼거리 아님으로 판정한 항목은 스냅샷에 없으므로 -excluded-cache(JSON)에 content_hash를 남겨
// 다음 실행에 다시 묻지 않는다.
//
// 예: go run ./cmd/ingest -out data/snapshot.json -max-gpt 150
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
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
	retain := flag.Int("retain-ended-days", ingest.DefaultRetainEndedDays, "끝난 항목 보존 기간(일). 지나면 제외·정리(0 = 끝나면 제외)")
	model := flag.String("model", openai.DefaultModel, "OpenAI 모델")
	priceIn := flag.Float64("price-in", 0.20, "비용 추정용 입력 토큰 단가(USD/1M)")
	priceOut := flag.Float64("price-out", 1.25, "비용 추정용 출력 토큰 단가(USD/1M)")
	exCache := flag.String("excluded-cache", "data/excluded.json", "GPT 볼거리 아님 판정 캐시(빈 값이면 안 씀)")
	showEx := flag.Int("show-excluded", 0, "제외 사유별로 제목 N개씩 출력(품질 점검용)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	srcs, err := buildSources(*srcFlag, *retain)
	if err != nil {
		return err
	}
	p := &ingest.Pipeline{
		Sources: srcs, MaxGPTCalls: *maxGPT, BatchSize: *batch, HorizonDays: *horizon, RetainEndedDays: *retain, Log: log,
	}
	if id, secret := os.Getenv("NCP_MAPS_CLIENT_ID"), os.Getenv("NCP_MAPS_CLIENT_SECRET"); id != "" && secret != "" {
		p.Geocoder = geo.NewNaverGeocoder(id, secret)
	} else {
		log.Warn("geocoder disabled (NCP_MAPS_CLIENT_ID/SECRET not set)")
	}
	if key := os.Getenv("KAKAO_REST_API_KEY"); key != "" {
		p.Places = geo.NewKakaoPlaces(key)
	} else {
		log.Warn("kakao place search disabled (KAKAO_REST_API_KEY not set)")
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
	var prior, previous []domain.Spot // previous: 이전 스냅샷 전체(스냅샷 모드에서 안 보인 항목 유지용)
	if dbURL != "" {
		if pg, err = postgres.Open(ctx, dbURL); err != nil {
			return err
		}
		defer func() { _ = pg.Close() }()
		if prior, err = pg.EnrichedSpots(ctx); err != nil {
			return err
		}
	} else if previous, _, err = snapshot.Read(*out); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("previous snapshot unreadable, starting without cache", "err", err)
	}
	if pg == nil {
		prior = previous
	}
	p.Prior = map[string]domain.Spot{}
	for _, sp := range prior {
		if sp.ContentHash != "" && sp.Summary != "" {
			p.Prior[sp.ContentHash] = sp
		}
	}

	if p.PriorExcluded, err = readExcluded(*exCache); err != nil {
		log.Warn("excluded cache unreadable, starting without it", "err", err)
	}

	started := time.Now()
	res := p.Run(ctx)
	fmt.Print(res.Report.String())
	printExcluded(res.Excluded, *showEx)
	cost := float64(res.Report.PromptTokens)/1e6**priceIn + float64(res.Report.CompletionTokens)/1e6**priceOut
	fmt.Printf("cache entries=%d  est_gpt_cost=$%.4f (model %s, 단가 가정 in $%.2f/out $%.2f per 1M)  elapsed=%s\n",
		len(p.Prior), cost, *model, *priceIn, *priceOut, time.Since(started).Round(time.Second))

	switch {
	case *dryRun:
		fmt.Println("dry-run: 저장하지 않음")
	case pg != nil:
		// 원천에서 빠진 행은 지우지 않는다(upsert만). 볼거리 아님 판정 행과 보존 기간 지난 행만 정리한다.
		for _, sp := range res.Spots {
			if err := pg.UpsertSpot(ctx, sp); err != nil {
				return err
			}
		}
		removed, err := pg.DeleteSpots(ctx, excludedIDs(res.Excluded))
		if err != nil {
			return err
		}
		pruned, err := pg.PruneEnded(ctx, ingest.RetainCutoff(time.Now(), *retain))
		if err != nil {
			return err
		}
		fmt.Printf("saved %d spots to postgres (excluded removed %d, pruned ended>%dd %d)\n", len(res.Spots), removed, *retain, pruned)
	default:
		ex := map[string]bool{}
		for _, id := range excludedIDs(res.Excluded) {
			ex[id] = true
		}
		spots, retained := ingest.MergeRetained(res.Spots, previous, ex, time.Now(), *retain)
		if err := snapshot.Write(*out, time.Now().UTC(), spots); err != nil {
			return err
		}
		fmt.Printf("saved %d spots to %s (이번 수집 %d + 이전 스냅샷에서 유지 %d)\n", len(spots), *out, len(res.Spots), retained)
	}
	if !*dryRun && *exCache != "" {
		n, err := writeExcluded(*exCache, res.Excluded)
		if err != nil {
			return err
		}
		fmt.Printf("saved %d gpt exclusions to %s\n", n, *exCache)
	}
	return nil
}

func excludedIDs(items []ingest.Excluded) []string {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

type excludedFile struct {
	Version int               `json:"version"`
	Items   []ingest.Excluded `json:"items"`
}

// readExcluded는 GPT 볼거리 아님 판정 캐시를 읽는다(content_hash → 사유). 파일이 없으면 빈 맵.
func readExcluded(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	} else if err != nil {
		return out, err
	}
	var f excludedFile
	if err := json.Unmarshal(b, &f); err != nil {
		return out, err
	}
	for _, it := range f.Items {
		if it.ContentHash != "" {
			out[it.ContentHash] = it.Reason
		}
	}
	return out, nil
}

// writeExcluded는 이번 실행의 GPT 판정 제외 항목만 저장한다(규칙 제외는 매번 다시 계산된다).
func writeExcluded(path string, items []ingest.Excluded) (int, error) {
	f := excludedFile{Version: 1, Items: []ingest.Excluded{}}
	for _, it := range items {
		if it.Reason == ingest.DropGPTNotAttraction && it.ContentHash != "" {
			f.Items = append(f.Items, it)
		}
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	return len(f.Items), os.WriteFile(path, b, 0o644)
}

func printExcluded(items []ingest.Excluded, n int) {
	if n <= 0 {
		return
	}
	shown := map[string]int{}
	for _, it := range items {
		if shown[it.Reason] < n {
			shown[it.Reason]++
			fmt.Printf("  excluded[%s] %s\n", it.Reason, it.Title)
		}
	}
}

func buildSources(list string, retainDays int) ([]ingest.Source, error) {
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
			sc := sources.NewSeoulCulture(seoulKey)
			sc.RetainEndedDays = retainDays
			out = append(out, sc)
		case "seoul_reservation":
			if err := need(seoulKey, "SEOUL_OPENAPI_KEY"); err != nil {
				return nil, err
			}
			out = append(out, sources.NewSeoulReservation(seoulKey))
		case "tourapi":
			if err := need(dataKey, "DATA_GO_KR_SERVICE_KEY"); err != nil {
				return nil, err
			}
			ta := sources.NewTourAPI(dataKey)
			ta.RetainEndedDays = retainDays
			out = append(out, ta)
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
