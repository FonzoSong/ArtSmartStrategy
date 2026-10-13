INSERT INTO system_rule_set (name, version, payload_json, enabled) VALUES ('艺数智策·内容运营规则集', '1.1', '{"name":"艺数智策·内容运营规则集","version":"1.1","description":"艺数智策项目内唯一只读规则来源，约束内容治理和未发布资产排期。","status_convention":["已发布","未发布"],"schedule_policy":{"planning_horizon_days":14,"platforms":["短视频平台A","短视频平台B","内容社区C","视频号"],"weekdays":["周一","周二","周三","周四","周五","周六","周日"],"time_windows":["12:30","18:30","19:30","20:00"],"max_per_platform_per_week":2,"max_per_day":2,"category_duration":{"演出宣传":{"min_minutes":0,"max_minutes":5,"reason":"按短时演出预告控制信息密度"},"技艺展示":{"min_minutes":1,"max_minutes":10,"reason":"保留完整技艺演示并控制单条时长"},"幕后记录":{"min_minutes":0,"max_minutes":8,"reason":"以排练或制作过程为主，控制单条时长"},"艺术科普":{"min_minutes":0,"max_minutes":6,"reason":"按单个知识点拆分内容"}}},"rules":[{"name":"高表现内容进入复盘池","priority":900,"enabled":true,"trigger":"content.create","logic":"all","conditions":[{"field":"lifecycle","operator":"eq","value":"已发布"},{"field":"views","operator":"gte","value":1000},{"field":"completion","operator":"gte","value":60}],"scope":{"platforms":[],"categories":[],"statuses":["已发布"]},"actions":[{"type":"add_tag","value":"高表现待复盘"},{"type":"append_note","value":"系统判定为高表现内容：建议复盘选题、开头和传播路径，并沉淀为可复用模板。"}]},{"name":"高互动内容重点复盘","priority":880,"enabled":true,"trigger":"content.update","logic":"any","conditions":[{"field":"likes","operator":"gte","value":300},{"field":"comments","operator":"gte","value":80},{"field":"completion","operator":"gte","value":70}],"scope":{"platforms":[],"categories":[],"statuses":["已发布"]},"actions":[{"type":"add_tag","value":"高互动"},{"type":"append_note","value":"互动或看完表现达到重点复盘阈值，请记录观众问题和有效表达。"}]},{"name":"低留存内容触发改进提醒","priority":820,"enabled":true,"trigger":"content.update","logic":"all","conditions":[{"field":"lifecycle","operator":"eq","value":"已发布"},{"field":"views","operator":"gte","value":300},{"field":"completion","operator":"lt","value":35}],"scope":{"platforms":[],"categories":[],"statuses":["已发布"]},"actions":[{"type":"add_tag","value":"低留存需优化"},{"type":"append_note","value":"看完比例偏低：下次优先检查前3秒、信息密度、时长和结尾承接。"}]},{"name":"未发布内容补齐负责人","priority":700,"enabled":true,"trigger":"content.create","logic":"all","conditions":[{"field":"lifecycle","operator":"eq","value":"未发布"},{"field":"owner","operator":"eq","value":""}],"scope":{"platforms":[],"categories":[],"statuses":["未发布"]},"actions":[{"type":"add_tag","value":"待分配负责人"},{"type":"append_note","value":"未发布内容尚未分配负责人，请在排期前补齐负责人和计划时间。"}]},{"name":"未发布内容统一治理","priority":650,"enabled":true,"trigger":"manual","logic":"all","conditions":[{"field":"lifecycle","operator":"neq","value":"已发布"}],"scope":{"platforms":[],"categories":[],"statuses":["未发布"]},"actions":[{"type":"add_tag","value":"未发布资产"},{"type":"append_note","value":"请在周会中确认该内容的下一步：继续制作、排期发布或归档。"}]}]}'::jsonb, TRUE) ON CONFLICT (name, version) DO NOTHING;

-- Synthetic, non-personal sample records for local/demo environments.
-- The PostgreSQL image executes this file only on a new data volume and only
-- when LOAD_EXAMPLE_DATA=true.

INSERT INTO team (id, name, created_at)
VALUES (1, '云锦戏曲社', NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO performance_booking (
    id, team_id, title, venue, starts_at, booking_status,
    performance_type, ticket_link, owner_user_id, notes, created_at
) VALUES
    ('00000000-0000-4000-8000-000000000101', 1, '春季折子戏专场', '云锦小剧场', CURRENT_TIMESTAMP + INTERVAL '18 days', '筹备中', '地方戏', '', NULL, '示例演出：含开场介绍、经典折子戏和谢幕环节。', NOW()),
    ('00000000-0000-4000-8000-000000000102', 1, '青年演员技艺展演', '城市艺术中心', CURRENT_TIMESTAMP + INTERVAL '35 days', '筹备中', '综合展演', '', NULL, '示例演出：展示唱念做打与后台准备过程。', NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO tag (team_id, name, normalized_name, color, is_active, created_by_user_id, created_at)
VALUES
    (1, '春季演出', '春季演出', '#b35d43', TRUE, NULL, NOW()),
    (1, '技艺展示', '技艺展示', '#397b74', TRUE, NULL, NOW()),
    (1, '后台纪实', '后台纪实', '#92713a', TRUE, NULL, NOW()),
    (1, '艺术科普', '艺术科普', '#5f6e9a', TRUE, NULL, NOW()),
    (1, '重点复盘', '重点复盘', '#885a8d', TRUE, NULL, NOW())
ON CONFLICT (team_id, normalized_name) DO NOTHING;

WITH numbered AS (
    SELECT n,
        CASE n % 8
            WHEN 0 THEN '已发布'
            WHEN 1 THEN '已发布'
            WHEN 2 THEN '已发布'
            WHEN 3 THEN '待制作'
            WHEN 4 THEN '待制作'
            WHEN 5 THEN '已制作'
            WHEN 6 THEN '待排期'
            ELSE '已排期'
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
    FROM generate_series(1, 120) AS n
), prepared AS (
    SELECT *,
        CASE category
            WHEN '演出宣传' THEN '春季折子戏专场预告'
            WHEN '技艺展示' THEN '水袖身段与节奏练习'
            WHEN '幕后记录' THEN '青年演员的一日排练记录'
            ELSE '一分钟读懂传统戏曲行当'
        END AS title_base
    FROM numbered
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
    '00000000-0000-4000-8000-' || lpad(n::text, 12, '0'),
    1,
    title_base || ' · 第' || lpad(n::text, 3, '0') || '条',
    category,
    platform,
    CASE WHEN workflow_state = '已发布'
         THEN CURRENT_TIMESTAMP - ((n * 3) % 150) * INTERVAL '1 day'
         ELSE CURRENT_TIMESTAMP END,
    35 + ((n * 17) % 480),
    CASE WHEN workflow_state = '已发布' THEN 800 + n * 137 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 25 + n * 4 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 5 + n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 8 + n * 2 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 3 + n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 42 + (n % 48) ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 41 + n * 8 ELSE 0 END,
    '[]',
    'example',
    CASE WHEN workflow_state = '已发布' THEN '已发布' ELSE '未发布' END,
    '竖屏成片；含字幕与片尾演出信息。',
    '',
    '待分配',
    CASE WHEN workflow_state = '已发布'
         THEN '示例复盘：比较开场信息密度、完播和互动表现。'
         ELSE '示例待办：补齐封面与发布文案，确认演出信息。' END,
    CASE WHEN workflow_state = '已发布' THEN n * 3 ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 20 + n ELSE 0 END,
    CASE WHEN workflow_state = '已发布' THEN 8 + n / 2 ELSE 0 END,
    '',
    workflow_state = '已发布' AND n % 2 = 0,
    NULL,
    CASE WHEN n % 2 = 0
         THEN '00000000-0000-4000-8000-000000000101'
         ELSE '00000000-0000-4000-8000-000000000102' END,
    NULL,
    workflow_state,
    '示例数据',
    NOW() - (n % 30) * INTERVAL '1 day',
    NOW() - (n % 30) * INTERVAL '1 day'
FROM prepared
WHERE NOT EXISTS (SELECT 1 FROM content WHERE team_id = 1);

INSERT INTO content_tag (content_id, tag_id)
SELECT c.id, t.id
FROM generate_series(1, 120) AS n
JOIN content AS c
  ON c.id = '00000000-0000-4000-8000-' || lpad(n::text, 12, '0')
JOIN tag AS t ON t.team_id = c.team_id
WHERE n % 4 <> 0
  AND t.normalized_name = CASE n % 4
      WHEN 1 THEN '春季演出'
      WHEN 2 THEN '技艺展示'
      WHEN 3 THEN '后台纪实'
      ELSE '艺术科普'
  END
ON CONFLICT DO NOTHING;

INSERT INTO content_tag (content_id, tag_id)
SELECT c.id, t.id
FROM generate_series(1, 120) AS n
JOIN content AS c
  ON c.id = '00000000-0000-4000-8000-' || lpad(n::text, 12, '0')
JOIN tag AS t ON t.team_id = c.team_id AND t.normalized_name = '重点复盘'
WHERE n % 5 = 0
ON CONFLICT DO NOTHING;

INSERT INTO schedule (
    id, team_id, recommendation_id, content_id, planned_date, planned_time,
    title, category, platform, owner, reason, status, source,
    created_by_user_id, actual_views, actual_likes, actual_comments,
    actual_saves, actual_shares, actual_completion, prediction_views_low,
    prediction_views_high, created_at, updated_at
)
SELECT
    '00000000-0000-4000-9000-' || lpad(n::text, 12, '0'),
    1, NULL, c.id, CURRENT_DATE + ((n % 24) + 1),
    CASE WHEN n % 2 = 0 THEN '18:30' ELSE '20:00' END,
    c.title, c.category, c.platform, c.owner,
    '示例排期：依据团队已发布样本和平台发布时间窗。',
    '待发布', CASE WHEN (n / 8) % 2 = 0 THEN 'auto' ELSE 'manual' END,
    NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    500, 3000, NOW(), NOW()
FROM generate_series(1, 120) AS n
JOIN content AS c
  ON c.id = '00000000-0000-4000-8000-' || lpad(n::text, 12, '0')
WHERE c.workflow_state = '已排期'
ON CONFLICT (id) DO NOTHING;

INSERT INTO benchmark (
    team_id, category, platform, completion_median, interaction_median, source_type
)
VALUES
    (1, '演出宣传', '短视频平台A', 68, 6.2, 'team_history'),
    (1, '技艺展示', '短视频平台B', 72, 7.1, 'team_history'),
    (1, '幕后记录', '内容社区C', 64, 5.8, 'team_history'),
    (1, '艺术科普', '视频号', 70, 6.5, 'team_history')
ON CONFLICT DO NOTHING;

INSERT INTO schema_migrations (version) VALUES ('0002_seed_rules')
ON CONFLICT (version) DO NOTHING;

-- The first team row above uses an explicit id, so advance the id sequence to
-- keep new team registrations working.
SELECT setval(pg_get_serial_sequence('team', 'id'), GREATEST(COALESCE((SELECT MAX(id) FROM team), 1), 1));
