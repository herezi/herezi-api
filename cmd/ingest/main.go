// Command ingest will run the collection pipeline (collect → normalize → geocode →
// GPT enrich → dedup → store) as a periodic batch job (TECH-PLAN.md §1).
// 수집기는 M1-a에서 구현한다. 지금은 자리만 잡아 둔 스텁이다.
package main

import (
	"log/slog"
	"os"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	log.Warn("ingest: not implemented")
}
