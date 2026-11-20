-- name: NearbySpots :many
-- 반경 안에서 기간 창과 겹치는 항목을 거리순으로. 끝난 행사(ends_at < now)는 제외.
-- 이 파일을 고치면 `make generate`(sqlc)로 sqlcdb/를 다시 만들어 함께 커밋한다.
SELECT
    id, source, source_id, title, summary, category,
    ST_Y(geog::geometry)::float8 AS lat,
    ST_X(geog::geometry)::float8 AS lng,
    address, place_name, starts_at, ends_at, is_free, image_url, source_url,
    ST_Distance(geog, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography)::float8 AS distance_m
FROM spots
WHERE ST_DWithin(geog, ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography, sqlc.arg(radius_m)::float8)
  AND (cardinality(sqlc.arg(categories)::text[]) = 0 OR category = ANY(sqlc.arg(categories)::text[]))
  AND (NOT sqlc.arg(free_only)::bool OR is_free IS TRUE)
  AND (ends_at IS NULL OR ends_at >= sqlc.arg(now)::timestamptz)
  AND starts_at <= sqlc.arg(window_to)::timestamptz
  AND COALESCE(ends_at, starts_at) >= sqlc.arg(window_from)::timestamptz
ORDER BY distance_m, id
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: GetSpot :one
-- 상세. 끝난 행사는 없는 것으로 취급한다.
SELECT
    id, source, source_id, title, summary, category,
    ST_Y(geog::geometry)::float8 AS lat,
    ST_X(geog::geometry)::float8 AS lng,
    address, place_name, starts_at, ends_at, is_free, image_url, source_url
FROM spots
WHERE id = sqlc.arg(id)
  AND (ends_at IS NULL OR ends_at >= sqlc.arg(now)::timestamptz);

-- name: UpsertSpot :exec
INSERT INTO spots (
    id, source, source_id, title, summary, category, geog,
    address, place_name, starts_at, ends_at, is_free, image_url, source_url
) VALUES (
    sqlc.arg(id), sqlc.arg(source), sqlc.arg(source_id), sqlc.arg(title), sqlc.arg(summary), sqlc.arg(category),
    ST_SetSRID(ST_MakePoint(sqlc.arg(lng)::float8, sqlc.arg(lat)::float8), 4326)::geography,
    sqlc.arg(address), sqlc.arg(place_name), sqlc.arg(starts_at), sqlc.narg(ends_at), sqlc.narg(is_free),
    sqlc.arg(image_url), sqlc.arg(source_url)
)
ON CONFLICT (id) DO UPDATE SET
    title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    category = EXCLUDED.category,
    geog = EXCLUDED.geog,
    address = EXCLUDED.address,
    place_name = EXCLUDED.place_name,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    is_free = EXCLUDED.is_free,
    image_url = EXCLUDED.image_url,
    source_url = EXCLUDED.source_url,
    updated_at = now();
