-- +goose Up
-- GPT 보강 입력(원문)의 해시. 같으면 이전 요약·분류·무료 판정을 재사용한다(TECH-PLAN.md §2, 이슈 #42).
ALTER TABLE spots ADD COLUMN content_hash text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE spots DROP COLUMN content_hash;
