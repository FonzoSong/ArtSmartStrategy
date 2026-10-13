-- Rename the shipped default system rule set while keeping its primary key,
-- version, enabled flag and payload. The old display name remains only as a
-- fallback until this migration is applied.
UPDATE system_rule_set
SET name = '系统规则集'
WHERE name = '艺数智策·内容运营规则集';

UPDATE system_rule_set
SET payload_json = jsonb_set(
	payload_json,
	'{name}',
	'"系统规则集"'::jsonb
)
WHERE name = '系统规则集'
  AND payload_json->>'name' = '艺数智策·内容运营规则集';
