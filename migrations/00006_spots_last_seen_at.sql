-- +goose Up
-- 수집에서 마지막으로 본 시각(이슈 #51). 원천 목록에서 빠져도 행을 지우지 않고 이 값만 갱신을 멈춘다.
-- 끝난 지 보존 기간(기본 90일)이 지난 행만 수집 배치가 정리한다(ends_at 기준).
ALTER TABLE spots ADD COLUMN IF NOT EXISTS last_seen_at timestamptz NOT NULL DEFAULT now();

-- 정리(DELETE ... WHERE ends_at < ?)용.
CREATE INDEX IF NOT EXISTS spots_ends_at_idx ON spots (ends_at);

-- +goose Down
DROP INDEX IF EXISTS spots_ends_at_idx;
ALTER TABLE spots DROP COLUMN IF EXISTS last_seen_at;
