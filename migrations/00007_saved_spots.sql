-- +goose Up
-- 저장(찜) 목록(이슈 #56, TECH-PLAN.md §4.1). 로그인한 사용자(Supabase Auth, sub = user_id)별로 보관한다.
--  - user_id: Supabase auth.users.id. auth 스키마에 FK를 걸지 않는다(CI의 일반 PostGIS에는 auth 스키마가 없다).
--    회원 탈퇴(DELETE /v1/me)는 API가 이 테이블의 행을 직접 지운다.
--  - spot_id: spots.id. 수집 배치가 종료 후 90일 지난 볼거리를 정리(PruneEnded)하거나 볼거리가 아니라고
--    지우면(DeleteSpots) 저장 행도 함께 지워진다(ON DELETE CASCADE) — 저장 목록에서도 사라진다.
--  - 사용자당 최대 500개는 API가 확인한다(store.MaxSavedPerUser).
CREATE TABLE IF NOT EXISTS saved_spots (
    user_id  uuid        NOT NULL,
    spot_id  text        NOT NULL REFERENCES spots (id) ON DELETE CASCADE,
    saved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, spot_id)
);

-- GET /v1/me/saved: 사용자별 저장 시각 최신순.
CREATE INDEX IF NOT EXISTS saved_spots_user_saved_at_idx ON saved_spots (user_id, saved_at DESC, spot_id);
-- spots 행 삭제 때 CASCADE가 saved_spots를 spot_id로 찾는다.
CREATE INDEX IF NOT EXISTS saved_spots_spot_id_idx ON saved_spots (spot_id);

-- Supabase Data API로 노출되지 않게 막는다(00005와 같은 규칙). API는 테이블 소유자로 붙어 RLS를 우회한다.
-- 정책을 만들지 않으므로 소유자 외에는 아무 행도 보이지 않는다("본인 행만"보다 엄격).
-- anon·authenticated 역할이 없는 일반 PostGIS(CI)에서는 회수를 건너뛴다.
ALTER TABLE saved_spots ENABLE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$
DECLARE
    r text;
BEGIN
    FOR r IN SELECT rolname FROM pg_roles WHERE rolname IN ('anon', 'authenticated') LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.saved_spots FROM %I', r);
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS saved_spots;
