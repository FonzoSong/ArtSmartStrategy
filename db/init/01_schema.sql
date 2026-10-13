-- PostgreSQL 16+ bootstrap. Executed by the official image through psql.
\getenv app_db_user APP_DB_USER
\getenv app_db_password APP_DB_PASSWORD
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'app_db_user', :'app_db_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app_db_user')
\gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'app_db_user')
\gexec
GRANT USAGE, CREATE ON SCHEMA public TO :"app_db_user";
SET ROLE :"app_db_user";
SET search_path TO public;

-- Generated from the current domain model; reviewed before use.

CREATE TABLE IF NOT EXISTS team (
	id SERIAL NOT NULL, 
	name VARCHAR(100) NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS ai_call_log (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	input_summary TEXT NOT NULL, 
	output_summary TEXT NOT NULL, 
	elapsed_ms INTEGER NOT NULL, 
	status VARCHAR(30) NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS ai_config (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	name VARCHAR(100) NOT NULL, 
	base_url TEXT NOT NULL, 
	api_key TEXT NOT NULL, 
	model VARCHAR(150) NOT NULL, 
	temperature FLOAT NOT NULL, 
	request_mode VARCHAR(20) NOT NULL, 
	auth_type VARCHAR(30) NOT NULL, 
	enabled BOOLEAN NOT NULL, 
	fallback_enabled BOOLEAN NOT NULL, 
	score_completion_weight FLOAT NOT NULL, 
	score_interaction_weight FLOAT NOT NULL, 
	min_sample INTEGER NOT NULL, 
	platform_weights_json TEXT NOT NULL, 
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS audit_log (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	actor VARCHAR(100) NOT NULL, 
	action VARCHAR(60) NOT NULL, 
	entity_type VARCHAR(50) NOT NULL, 
	entity_id VARCHAR(80) NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	detail TEXT NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS benchmark (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	category VARCHAR(40) NOT NULL, 
	platform VARCHAR(100) NOT NULL, 
	completion_median FLOAT NOT NULL, 
	interaction_median FLOAT NOT NULL, 
	source_type VARCHAR(30) NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS hypothesis (
	id VARCHAR(36) NOT NULL, 
	team_id INTEGER NOT NULL, 
	statement TEXT NOT NULL, 
	result TEXT NOT NULL, 
	status VARCHAR(20) NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS recommendation (
	id VARCHAR(36) NOT NULL, 
	team_id INTEGER NOT NULL, 
	generated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	payload_json TEXT NOT NULL, 
	source_model VARCHAR(100) NOT NULL, 
	source_label VARCHAR(120) NOT NULL, 
	confidence VARCHAR(20) NOT NULL, 
	reference_count INTEGER NOT NULL, 
	analysis_status VARCHAR(40) NOT NULL, 
	fallback_reason TEXT NOT NULL, 
	adopted BOOLEAN NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS user_account (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	email VARCHAR(254) NOT NULL, 
	name VARCHAR(100) NOT NULL, 
	role_name VARCHAR(20) NOT NULL, 
	password_hash VARCHAR(256) NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id)
);

CREATE TABLE IF NOT EXISTS content_rule (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	name VARCHAR(120) NOT NULL, 
	priority INTEGER NOT NULL, 
	enabled BOOLEAN NOT NULL, 
	trigger VARCHAR(40) NOT NULL, 
	logic VARCHAR(10) NOT NULL, 
	conditions_json TEXT NOT NULL, 
	scope_json TEXT NOT NULL, 
	actions_json TEXT NOT NULL, 
	rule_type VARCHAR(40) NOT NULL, 
	rule_content TEXT NOT NULL, 
	content_type VARCHAR(40) NOT NULL, 
	workflow_state VARCHAR(20) NOT NULL, 
	notes TEXT NOT NULL, 
	created_by_user_id INTEGER, 
	updated_by_user_id INTEGER, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(created_by_user_id) REFERENCES user_account (id), 
	FOREIGN KEY(updated_by_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS performance_booking (
	id VARCHAR(36) NOT NULL, 
	team_id INTEGER NOT NULL, 
	title VARCHAR(300) NOT NULL, 
	venue VARCHAR(200) NOT NULL, 
	starts_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	booking_status VARCHAR(30) NOT NULL, 
	performance_type VARCHAR(60) NOT NULL DEFAULT '', 
	ticket_link TEXT NOT NULL DEFAULT '', 
	owner_user_id INTEGER, 
	notes TEXT NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	CONSTRAINT ck_performance_booking_status CHECK (booking_status IN ('筹备中', '预热中', '已演出', '已取消')), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(owner_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS tag (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	name VARCHAR(50) NOT NULL, 
	normalized_name VARCHAR(100) NOT NULL, 
	color VARCHAR(20) NOT NULL, 
	is_active BOOLEAN NOT NULL, 
	created_by_user_id INTEGER, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	CONSTRAINT uq_tag_team_normalized_name UNIQUE (team_id, normalized_name), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(created_by_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS user_session (
	id VARCHAR(36) NOT NULL, 
	user_id INTEGER NOT NULL, 
	expires_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	revoked BOOLEAN NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS content (
	id VARCHAR(36) NOT NULL, 
	team_id INTEGER NOT NULL, 
	title VARCHAR(300) NOT NULL, 
	category VARCHAR(40) NOT NULL, 
	platform VARCHAR(100) NOT NULL, 
	published_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	duration INTEGER NOT NULL, 
	views INTEGER NOT NULL, 
	likes INTEGER NOT NULL, 
	comments INTEGER NOT NULL, 
	saves INTEGER NOT NULL, 
	shares INTEGER NOT NULL, 
	completion FLOAT NOT NULL, 
	total_interactions INTEGER NOT NULL, 
	tags_json TEXT NOT NULL, 
	source VARCHAR(30) NOT NULL, 
	lifecycle VARCHAR(20) NOT NULL, 
	materials TEXT NOT NULL, 
	cover_url TEXT NOT NULL, 
	owner VARCHAR(100) NOT NULL, 
	notes TEXT NOT NULL, 
	profile_visits INTEGER NOT NULL, 
	follows INTEGER NOT NULL, 
	attendance INTEGER NOT NULL, 
	tickets INTEGER NOT NULL, 
	comment_text TEXT NOT NULL, 
	adopted BOOLEAN NOT NULL, 
	recommendation_id VARCHAR(36), 
	performance_id VARCHAR(36), 
	owner_user_id INTEGER, 
	workflow_state VARCHAR(20) NOT NULL, 
	created_by VARCHAR(100) NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	CONSTRAINT ck_content_lifecycle_two_states CHECK (lifecycle IN ('已发布', '未发布')), 
	CONSTRAINT ck_content_workflow_state_values CHECK (workflow_state IN ('待制作', '已制作', '待排期', '已排期', '已发布')), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(recommendation_id) REFERENCES recommendation (id), 
	FOREIGN KEY(performance_id) REFERENCES performance_booking (id), 
	FOREIGN KEY(owner_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS rule_execution_log (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	rule_id INTEGER, 
	rule_name VARCHAR(120) NOT NULL, 
	content_id VARCHAR(36) NOT NULL, 
	trigger VARCHAR(40) NOT NULL, 
	result VARCHAR(20) NOT NULL, 
	detail TEXT NOT NULL, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(rule_id) REFERENCES content_rule (id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS content_tag (
	content_id VARCHAR(36) NOT NULL, 
	tag_id INTEGER NOT NULL, 
	PRIMARY KEY (content_id, tag_id), 
	FOREIGN KEY(content_id) REFERENCES content (id) ON DELETE CASCADE, 
	FOREIGN KEY(tag_id) REFERENCES tag (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS content_workflow_event (
	id SERIAL NOT NULL, 
	team_id INTEGER NOT NULL, 
	content_id VARCHAR(36) NOT NULL, 
	from_state VARCHAR(20) NOT NULL, 
	to_state VARCHAR(20) NOT NULL, 
	source VARCHAR(20) NOT NULL, 
	actor_user_id INTEGER, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(content_id) REFERENCES content (id) ON DELETE CASCADE, 
	FOREIGN KEY(actor_user_id) REFERENCES user_account (id)
);

CREATE TABLE IF NOT EXISTS schedule (
	id VARCHAR(36) NOT NULL, 
	team_id INTEGER NOT NULL, 
	recommendation_id VARCHAR(36), 
	content_id VARCHAR(36), 
	planned_date DATE NOT NULL, 
	planned_time VARCHAR(10) NOT NULL, 
	title VARCHAR(300) NOT NULL, 
	category VARCHAR(40) NOT NULL, 
	platform VARCHAR(100) NOT NULL, 
	owner VARCHAR(100) NOT NULL, 
	reason TEXT NOT NULL, 
	status VARCHAR(20) NOT NULL, 
	source VARCHAR(20) NOT NULL, 
	created_by_user_id INTEGER, 
	actual_views INTEGER, 
	actual_likes INTEGER, 
	actual_comments INTEGER, 
	actual_saves INTEGER, 
	actual_shares INTEGER, 
	actual_completion FLOAT, 
	prediction_views_low INTEGER, 
	prediction_views_high INTEGER, 
	created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(team_id) REFERENCES team (id), 
	FOREIGN KEY(recommendation_id) REFERENCES recommendation (id), 
	FOREIGN KEY(content_id) REFERENCES content (id), 
	FOREIGN KEY(created_by_user_id) REFERENCES user_account (id)
);

CREATE INDEX IF NOT EXISTS ix_ai_call_log_team_id ON ai_call_log (team_id);

CREATE INDEX IF NOT EXISTS ix_ai_config_team_id ON ai_config (team_id);

CREATE INDEX IF NOT EXISTS ix_audit_log_team_id ON audit_log (team_id);

CREATE INDEX IF NOT EXISTS ix_benchmark_team_id ON benchmark (team_id);

CREATE INDEX IF NOT EXISTS ix_hypothesis_team_id ON hypothesis (team_id);

CREATE INDEX IF NOT EXISTS ix_recommendation_team_id ON recommendation (team_id);

CREATE UNIQUE INDEX IF NOT EXISTS ix_user_account_email ON user_account (email);

CREATE INDEX IF NOT EXISTS ix_user_account_team_id ON user_account (team_id);

CREATE INDEX IF NOT EXISTS ix_content_rule_team_id ON content_rule (team_id);

CREATE INDEX IF NOT EXISTS ix_performance_booking_owner_user_id ON performance_booking (owner_user_id);

CREATE INDEX IF NOT EXISTS ix_performance_booking_starts_at ON performance_booking (starts_at);

CREATE INDEX IF NOT EXISTS ix_performance_booking_team_id ON performance_booking (team_id);

CREATE INDEX IF NOT EXISTS ix_performance_booking_team_starts_at ON performance_booking (team_id, starts_at);

CREATE INDEX IF NOT EXISTS ix_tag_team_id ON tag (team_id);

CREATE INDEX IF NOT EXISTS ix_user_session_user_id ON user_session (user_id);

CREATE INDEX IF NOT EXISTS ix_content_team_id ON content (team_id);

CREATE INDEX IF NOT EXISTS ix_content_performance_id ON content (performance_id);

CREATE INDEX IF NOT EXISTS ix_content_owner_user_id ON content (owner_user_id);

CREATE INDEX IF NOT EXISTS ix_content_workflow_state ON content (workflow_state);

CREATE INDEX IF NOT EXISTS ix_content_published_at ON content (published_at);

CREATE INDEX IF NOT EXISTS ix_rule_execution_log_team_id ON rule_execution_log (team_id);

CREATE INDEX IF NOT EXISTS ix_content_workflow_event_content_id ON content_workflow_event (content_id);

CREATE INDEX IF NOT EXISTS ix_content_workflow_event_team_id ON content_workflow_event (team_id);

CREATE INDEX IF NOT EXISTS ix_schedule_team_id ON schedule (team_id);


CREATE TABLE IF NOT EXISTS system_rule_set (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(160) NOT NULL,
    version VARCHAR(40) NOT NULL,
    payload_json JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, version)
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(160) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO schema_migrations (version) VALUES ('0001_initial')
ON CONFLICT (version) DO NOTHING;
RESET ROLE;
