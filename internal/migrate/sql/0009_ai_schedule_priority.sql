ALTER TABLE schedule ADD COLUMN IF NOT EXISTS priority VARCHAR(12) NOT NULL DEFAULT 'medium';
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS priority_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE schedule ADD COLUMN IF NOT EXISTS risk_notes TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS ix_schedule_recommendation_id ON schedule (recommendation_id);
