DELETE FROM system_rule_set AS old
WHERE old.name = '艺数智策·内容运营规则集'
  AND old.version = '1.0'
  AND EXISTS (
      SELECT 1 FROM system_rule_set AS current
      WHERE current.name = old.name AND current.version = '1.1'
  );

UPDATE system_rule_set AS rules
SET version = '1.1',
    payload_json = jsonb_set(
        rules.payload_json,
        '{version}',
        '"1.1"'::jsonb,
        TRUE
    ),
    updated_at = NOW()
WHERE rules.name = '艺数智策·内容运营规则集'
  AND rules.version = '1.0';

UPDATE system_rule_set AS rules
SET payload_json = jsonb_set(
        rules.payload_json,
        '{rules}',
        (
            SELECT COALESCE(jsonb_agg(item.rule ORDER BY item.position), '[]'::jsonb)
            FROM jsonb_array_elements(rules.payload_json->'rules')
                 WITH ORDINALITY AS item(rule, position)
            WHERE item.rule->>'trigger' <> 'content.import'
        ),
        TRUE
    ),
    updated_at = NOW()
WHERE rules.name = '艺数智策·内容运营规则集'
  AND rules.version = '1.1'
  AND jsonb_path_exists(
      rules.payload_json,
      '$.rules[*] ? (@.trigger == "content.import")'
  );
