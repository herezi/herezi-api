-- +goose Up
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE spots (
    id          text PRIMARY KEY,               -- "<source>:<source_id>"
    source      text NOT NULL,
    source_id   text NOT NULL,
    title       text NOT NULL,
    summary     text NOT NULL DEFAULT '',
    category    text NOT NULL,                  -- api/openapi.yaml Category와 같은 9종
    geog        geography(Point, 4326) NOT NULL,
    address     text NOT NULL DEFAULT '',
    place_name  text NOT NULL DEFAULT '',
    starts_at   timestamptz NOT NULL,
    ends_at     timestamptz,
    is_free     boolean,                        -- NULL = 모름. 무료는 카테고리가 아니라 필터
    image_url   text NOT NULL DEFAULT '',
    source_url  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, source_id),
    CONSTRAINT spots_category_check CHECK (category IN (
        'nature', 'festival', 'performance', 'exhibition', 'popup',
        'food', 'family', 'pet', 'sports'))
);

CREATE INDEX spots_geog_gist ON spots USING gist (geog);
CREATE INDEX spots_time_idx ON spots (starts_at, ends_at);

-- +goose Down
DROP TABLE spots;
