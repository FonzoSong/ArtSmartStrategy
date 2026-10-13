-- Custom (team-editable) rule sets plus the team's effective rule-set selection.
-- System rule sets stay in system_rule_set and are never editable through the API.

CREATE TABLE IF NOT EXISTS content_rule_set (
	id SERIAL NOT NULL,
	team_id INTEGER NOT NULL,
	name VARCHAR(120) NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_by_user_id INTEGER,
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
	PRIMARY KEY (id),
	CONSTRAINT uq_content_rule_set_team_name UNIQUE (team_id, name),
	FOREIGN KEY(team_id) REFERENCES team (id),
	FOREIGN KEY(created_by_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS content_rule_set_item (
	id SERIAL NOT NULL,
	rule_set_id INTEGER NOT NULL,
	name VARCHAR(120) NOT NULL,
	priority INTEGER NOT NULL DEFAULT 100,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	rule_type VARCHAR(40) NOT NULL,
	rule_content TEXT NOT NULL DEFAULT '{}',
	content_type VARCHAR(40) NOT NULL DEFAULT '',
	workflow_state VARCHAR(20) NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
	PRIMARY KEY (id),
	FOREIGN KEY(rule_set_id) REFERENCES content_rule_set (id) ON DELETE CASCADE
);

-- Exactly one target is selected per team: a system rule set or a custom one.
CREATE TABLE IF NOT EXISTS team_rule_selection (
	team_id INTEGER NOT NULL,
	system_rule_set_id BIGINT,
	content_rule_set_id INTEGER,
	updated_by_user_id INTEGER,
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
	PRIMARY KEY (team_id),
	CONSTRAINT ck_team_rule_selection_target CHECK ((system_rule_set_id IS NULL) <> (content_rule_set_id IS NULL)),
	FOREIGN KEY(team_id) REFERENCES team (id) ON DELETE CASCADE,
	FOREIGN KEY(system_rule_set_id) REFERENCES system_rule_set (id),
	FOREIGN KEY(content_rule_set_id) REFERENCES content_rule_set (id),
	FOREIGN KEY(updated_by_user_id) REFERENCES user_account (id)
);

CREATE INDEX IF NOT EXISTS ix_content_rule_set_team_id ON content_rule_set (team_id);
CREATE INDEX IF NOT EXISTS ix_content_rule_set_item_set_id ON content_rule_set_item (rule_set_id);

-- Existing team validation rules become a custom rule set so nothing is lost and
-- the team can keep editing them in the new structure.
INSERT INTO content_rule_set (team_id, name, description)
SELECT DISTINCT cr.team_id, '团队校验规则集', '由历史团队校验规则自动整理'
FROM content_rule cr
ON CONFLICT (team_id, name) DO NOTHING;

INSERT INTO content_rule_set_item (rule_set_id, name, priority, enabled, rule_type, rule_content, content_type, workflow_state, notes)
SELECT s.id, cr.name, cr.priority, cr.enabled, cr.rule_type, cr.rule_content, cr.content_type, cr.workflow_state, cr.notes
FROM content_rule cr
JOIN content_rule_set s ON s.team_id = cr.team_id AND s.name = '团队校验规则集'
WHERE NOT EXISTS (
	SELECT 1 FROM content_rule_set_item item
	WHERE item.rule_set_id = s.id AND item.name = cr.name
);
