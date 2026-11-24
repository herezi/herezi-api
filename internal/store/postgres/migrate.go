package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 드라이버 "pgx"
	"github.com/pressly/goose/v3"

	"github.com/herezi/herezi-api/migrations"
)

// Migrate는 migrations/(goose SQL, embed)를 최신까지 적용하고 적용한 버전 수를 돌려준다.
// cmd/api가 MIGRATE_ON_START=1일 때만 부른다. 에러에 접속 문자열을 넣지 않는다.
func Migrate(ctx context.Context, databaseURL string) (int, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return 0, fmt.Errorf("postgres: migrate: open failed")
	}
	defer func() { _ = db.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return 0, fmt.Errorf("postgres: migrate: %w", err)
	}
	res, err := p.Up(ctx)
	if err != nil {
		return len(res), fmt.Errorf("postgres: migrate up: %w", err)
	}
	return len(res), nil
}
