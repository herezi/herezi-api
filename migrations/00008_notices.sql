-- +goose Up
-- 공지사항(이슈 #80). 웹 공지 페이지(https://herezi.github.io/notices/)가 GET /v1/notices로 읽는다.
-- 작성·수정은 Supabase 대시보드 Table Editor에서 행을 추가·수정한다(관리자 API 없음, docs/ops/notices.md).
--  - body: 마크다운 원문. 렌더링은 웹 페이지가 한다.
--  - published_at: 게시 시각. 미래 시각이면 그때까지 보이지 않는다(예약 게시).
--  - is_hidden: true면 목록·상세 모두에서 숨긴다(삭제 대신).
--  - updated_at: 행을 고치면 트리거가 now()로 바꾼다(Table Editor에서 고쳐도 남도록).
CREATE TABLE IF NOT EXISTS notices (
    id           bigserial   PRIMARY KEY,
    title        text        NOT NULL,
    body         text        NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    is_hidden    boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- GET /v1/notices: 게시 시각 최신순(같으면 id 큰 것 먼저).
CREATE INDEX IF NOT EXISTS notices_published_at_idx ON notices (published_at DESC, id DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notices_set_updated_at() RETURNS trigger
LANGUAGE plpgsql
SET search_path = public, pg_temp
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS notices_set_updated_at ON notices;
CREATE TRIGGER notices_set_updated_at BEFORE UPDATE ON notices
    FOR EACH ROW EXECUTE FUNCTION notices_set_updated_at();

-- Supabase Data API로 노출되지 않게 막는다(00005·00007과 같은 규칙). API는 테이블 소유자로 붙어 RLS를 우회한다.
-- 정책을 만들지 않으므로 소유자 외에는 아무 행도 보이지 않는다. 공개 읽기는 cmd/api(GET /v1/notices)로만 한다.
-- anon·authenticated 역할이 없는 일반 PostGIS(CI)에서는 회수를 건너뛴다.
ALTER TABLE notices ENABLE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$
DECLARE
    r text;
BEGIN
    FOR r IN SELECT rolname FROM pg_roles WHERE rolname IN ('anon', 'authenticated') LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.notices FROM %I', r);
        EXECUTE format('REVOKE ALL ON SEQUENCE public.notices_id_seq FROM %I', r);
        EXECUTE format('REVOKE ALL ON FUNCTION public.notices_set_updated_at() FROM %I', r);
    END LOOP;
END
$$;
-- +goose StatementEnd

-- 첫 공지. 시각은 KST 2026-11-25 00:00으로 고정한다(다시 적용해도 같은 행이 되도록 제목·시각으로 확인).
INSERT INTO notices (title, body, published_at)
SELECT '히어지 테스트를 시작했어요',
       E'안녕하세요, 히어지예요.\n\n' ||
       E'iOS·Android 앱의 **내부 테스트**를 시작했어요. 지금 내 주변에서 볼 수 있는 축제·공연·전시·팝업을 한눈에 보여 드리려고 해요.\n\n' ||
       E'- 볼거리 정보는 공공데이터 등 **공개된 자료**를 모아 보여 드려요. 그래서 실제 일정·장소와 다를 수 있어요.\n' ||
       E'- 다른 점을 발견하면 앱에서 **"정보가 달라요"**를 눌러 알려 주세요. 확인해서 고칠게요.\n\n' ||
       E'테스트에 함께해 주셔서 고마워요!',
       '2026-11-25 00:00:00+09'
WHERE NOT EXISTS (
    SELECT 1 FROM notices WHERE title = '히어지 테스트를 시작했어요' AND published_at = '2026-11-25 00:00:00+09'
);

-- +goose Down
DROP TABLE IF EXISTS notices;
DROP FUNCTION IF EXISTS notices_set_updated_at();
