-- +goose Up
-- 원본 소스에서 마지막으로 가져온 시각. 앱 상세의 "갱신 시각" (api/openapi.yaml Spot.fetched_at).
ALTER TABLE spots ADD COLUMN fetched_at timestamptz NOT NULL DEFAULT now();

-- 지도 영역(bbox) 조회용. 점 데이터라 평면 bbox 겹침(geometry &&)이 정확하고 인덱스를 탄다.
CREATE INDEX spots_geom_gist ON spots USING gist ((geog::geometry));

-- +goose Down
DROP INDEX spots_geom_gist;
ALTER TABLE spots DROP COLUMN fetched_at;
