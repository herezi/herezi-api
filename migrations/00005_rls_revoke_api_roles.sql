-- +goose Up
-- Supabase Data API(PostgREST)로 테이블이 노출되지 않게 막는다(이슈 #7).
-- 앱은 이 DB에 직접 붙지 않고 cmd/api만 쓴다(테이블 소유자라 RLS를 우회).
--  - public 스키마에서 현재 역할이 소유한 모든 테이블(spots, goose_db_version 등)에 RLS를 켠다.
--    PostGIS의 spatial_ref_sys처럼 소유자가 다른 테이블은 건너뛴다(ALTER 권한 없음).
--  - Supabase 역할(anon, authenticated)이 있으면 현재 역할 소유 테이블·뷰·시퀀스 권한을 모두 회수하고,
--    앞으로 만들 객체의 기본 권한에서도 뺀다. 일반 PostGIS(CI)에는 이 역할이 없으므로 건너뛴다.
-- 여러 번 실행해도 결과가 같다(운영 DB에는 수동으로 먼저 적용됨). 새 테이블을 추가하는
-- 마이그레이션은 같은 파일에서 ENABLE ROW LEVEL SECURITY를 함께 쓴다.
-- (spatial_ref_sys·geometry_columns·geography_columns는 Supabase 소유 PostGIS 참조 데이터라 권한이 남는다.)
-- +goose StatementBegin
DO $$
DECLARE
    obj   record;
    r     text;
    roles text[];
BEGIN
    SELECT array_agg(rolname) INTO roles
    FROM pg_roles WHERE rolname IN ('anon', 'authenticated');

    FOR obj IN
        SELECT c.relname, c.relkind
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
          AND pg_get_userbyid(c.relowner) = current_user
    LOOP
        IF obj.relkind IN ('r', 'p') THEN
            EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', obj.relname);
        END IF;
        FOREACH r IN ARRAY coalesce(roles, '{}') LOOP
            IF obj.relkind = 'S' THEN
                EXECUTE format('REVOKE ALL ON SEQUENCE public.%I FROM %I', obj.relname, r);
            ELSE
                EXECUTE format('REVOKE ALL ON TABLE public.%I FROM %I', obj.relname, r);
            END IF;
        END LOOP;
    END LOOP;

    FOREACH r IN ARRAY coalesce(roles, '{}') LOOP
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM %I', r);
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM %I', r);
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 보안 설정이라 되돌리지 않는다(권한을 다시 열지 않음).
SELECT 1;
