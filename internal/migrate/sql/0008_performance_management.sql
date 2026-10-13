-- 正式演出管理：在既有 performance_booking 实体上安全扩展，不新建平行演出表。
--
-- 1. 补充正式演出所需字段（剧种/演出类型、票档或购票链接）。
-- 2. 统一演出状态口径为：筹备中、预热中、已演出、已取消。
--    历史值（已预订、已确认、待确认等）按“仍在筹备”迁移为筹备中。
-- 3. 演出是否已开演由服务端按 starts_at 判定，不再把预订状态当作发布状态。

ALTER TABLE performance_booking ADD COLUMN IF NOT EXISTS performance_type VARCHAR(60) NOT NULL DEFAULT '';
ALTER TABLE performance_booking ADD COLUMN IF NOT EXISTS ticket_link TEXT NOT NULL DEFAULT '';

UPDATE performance_booking
SET booking_status = '筹备中'
WHERE booking_status IS NULL
   OR booking_status NOT IN ('筹备中', '预热中', '已演出', '已取消');

UPDATE performance_booking
SET booking_status = '已演出'
WHERE booking_status IN ('筹备中', '预热中')
  AND starts_at <= LOCALTIMESTAMP;

ALTER TABLE performance_booking DROP CONSTRAINT IF EXISTS ck_performance_booking_status;
ALTER TABLE performance_booking ADD CONSTRAINT ck_performance_booking_status
    CHECK (booking_status IN ('筹备中', '预热中', '已演出', '已取消'));

CREATE INDEX IF NOT EXISTS ix_performance_booking_team_starts_at
    ON performance_booking (team_id, starts_at);
