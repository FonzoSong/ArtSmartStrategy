-- 可选示例内容：把一个团队补足到 120 条示例内容资产。
--
-- 用途：新环境首次初始化只给内置示例团队（team 1）填充 120 条示例内容；如果管理员
--       在 Web 上新建了自己的团队，可用本脚本把该团队也补到 120 条，便于查看列表、
--       分页、筛选、效果分析与排期等页面在真实数据量下的表现。
--
-- 特性：
--   * 幂等：只插入“补足到 120 条”所需的行，重复执行不会超过 120 条，也不会改动已有行。
--   * 安全：只写入 source='example' 的示例内容与示例标签，不删除、不更新任何既有数据，
--           不创建账号、演出或排期。
--   * 明确来源：所有行都标记来源为示例，标题与备注写明“示例数据”，便于与真实运营数据区分。
--
-- 用法（team_id 必填）：
--   docker compose exec -T db psql -U arts_admin -d arts_smart \
--     -v team_id=2 -v ON_ERROR_STOP=1 -f /docker-entrypoint-sql/03_example_team_contents.sql

\if :{?team_id}
\else
\echo '缺少参数 team_id。示例：psql -v team_id=2 -f db/sql/03_example_team_contents.sql'
\quit
\endif

BEGIN;

-- 示例标签：与内置样例保持一致，便于按标签筛选。
INSERT INTO tag (team_id, name, normalized_name, color, is_active, created_by_user_id, created_at)
VALUES
    (:team_id, '春季演出', '春季演出', '#b35d43', TRUE, NULL, NOW()),
    (:team_id, '技艺展示', '技艺展示', '#397b74', TRUE, NULL, NOW()),
    (:team_id, '后台纪实', '后台纪实', '#92713a', TRUE, NULL, NOW()),
    (:team_id, '艺术科普', '艺术科普', '#5f6e9a', TRUE, NULL, NOW()),
    (:team_id, '重点复盘', '重点复盘', '#885a8d', TRUE, NULL, NOW())
ON CONFLICT (team_id, normalized_name) DO NOTHING;

-- 计算需要补足的行数，然后按 n = 已有条数 + 序号 生成新行，保证总数正好是 120。
WITH target AS (SELECT 120 AS total),
existing AS (
    SELECT count(*) AS n
    FROM content, target
    WHERE content.team_id = :team_id AND content.source = 'example'
),
needed AS (
    SELECT GREATEST(0, target.total - existing.n) AS n, existing.n AS already
    FROM target, existing
),
numbered AS (
    SELECT needed.already + g AS n
    FROM needed, LATERAL generate_series(1, needed.n) AS g
),
prepared AS (
    SELECT n,
        CASE n % 8
            WHEN 0 THEN '已发布'
            WHEN 1 THEN '已发布'
            WHEN 2 THEN '已发布'
            WHEN 3 THEN '待制作'
            WHEN 4 THEN '待制作'
            WHEN 5 THEN '待制作'
            WHEN 6 THEN '已制作'
            ELSE '待排期'
        END AS workflow_state,
        CASE n % 4
            WHEN 1 THEN '演出宣传'
            WHEN 2 THEN '技艺展示'
            WHEN 3 THEN '幕后记录'
            ELSE '艺术科普'
        END AS category,
        CASE n % 4
            WHEN 1 THEN '短视频平台A'
            WHEN 2 THEN '短视频平台B'
            WHEN 3 THEN '内容社区C'
            ELSE '视频号'
        END AS platform
    FROM numbered
),
titled AS (
    SELECT *,
        CASE category
            WHEN '演出宣传' THEN '折子戏专场预热'
            WHEN '技艺展示' THEN '水袖身段与节奏练习'
            WHEN '幕后记录' THEN '青年演员的一日排练记录'
            ELSE '一分钟读懂传统戏曲行当'
        END AS title_base
    FROM prepared
)
INSERT INTO content (
    id, team_id, title, category, platform, published_at, duration,
    views, likes, comments, saves, shares, completion, total_interactions,
    tags_json, source, lifecycle, materials, cover_url, owner, notes,
    profile_visits, follows, attendance, tickets, comment_text, adopted,
    recommendation_id, performance_id, owner_user_id, workflow_state,
    created_by, created_at, updated_at
)
SELECT
    -- 36 位、含连字符的稳定 ID：团队号 + 序号，重复执行会命中同一批 ID。
    '00000000-0000-4000-8000-' || lpad((:team_id)::text, 2, '0') || lpad(n::text, 10, '0'),
    :team_id,
    title_base || ' · 示例第' || lpad(n::text, 3, '0') || '条',
    category,
    platform,
    CASE WHEN workflow_state = '已发布'
         THEN CURRENT_TIMESTAMP - ((n * 3) % 150) * INTERVAL '1 day'
         ELSE CURRENT_TIMESTAMP + ((n * 2) % 20) * INTERVAL '1 day'
    END,
    35 + ((n * 17) % 480),
    CASE WHEN workflow_state = '已发布' THEN 800 + n * 137 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 25 + n * 4 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 5 + n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 8 + n * 2 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 3 + n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 42 + (n % 48) ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN (25 + n * 4) + (5 + n) + (8 + n * 2) + (3 + n) ELSE 0 END,
    '[]',
    'example',
    CASE WHEN workflow_state = '已发布' THEN '已发布' ELSE '未发布' END,
    '示例素材：仅用于界面与流程演示',
    '',
    '示例负责人',
    '示例数据（非真实运营记录），可在确认后删除',
    CASE WHEN workflow_state = '已发布' THEN 500 + n * 11 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 120 + n * 3 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 40 + (n % 90) ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 30 + (n % 70) ELSE 0 END,
    '',
    FALSE,
    NULL,
    NULL,
    NULL,
    workflow_state,
    '示例数据',
    NOW(),
    NOW()
FROM titled
ON CONFLICT (id) DO NOTHING;

-- 若该团队已有正式演出，把示例内容按顺序分散关联到这些演出，便于查看“预热视频聚合”。
-- 只更新由本脚本生成的示例行，且不覆盖已有关联。
WITH shows AS (
    SELECT id, row_number() OVER (ORDER BY starts_at, id) AS rn, count(*) OVER () AS total
    FROM performance_booking
    WHERE team_id = :team_id
)
UPDATE content c
SET performance_id = shows.id
FROM shows
WHERE c.team_id = :team_id
  AND c.source = 'example'
  AND c.performance_id IS NULL
  AND c.id LIKE '00000000-0000-4000-8000-' || lpad((:team_id)::text, 2, '0') || '%'
  AND shows.total > 0
  AND shows.rn = (right(c.id, 10)::bigint % shows.total) + 1;

COMMIT;

SELECT (SELECT count(*) FROM content WHERE team_id = :team_id AND source = 'example') AS example_contents,
       (SELECT count(*) FROM content WHERE team_id = :team_id) AS total_contents;
