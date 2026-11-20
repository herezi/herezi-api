-- +goose Up
-- 키워드 검색(GET /v1/spots?q=)용. 제목·장소명·주소 ILIKE '%q%'가 인덱스를 타도록 pg_trgm GIN 인덱스.
-- 트라이그램이라 3글자 이상 검색어에서 효과적이다(1~2글자는 순차 스캔이지만 반경 조건과 함께라 감당 가능).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX spots_title_trgm ON spots USING gin (title gin_trgm_ops);
CREATE INDEX spots_place_name_trgm ON spots USING gin (place_name gin_trgm_ops);
CREATE INDEX spots_address_trgm ON spots USING gin (address gin_trgm_ops);

-- +goose Down
DROP INDEX spots_address_trgm;
DROP INDEX spots_place_name_trgm;
DROP INDEX spots_title_trgm;
-- 확장은 다른 객체가 쓸 수 있어 내리지 않는다.
