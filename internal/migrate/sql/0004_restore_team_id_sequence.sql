-- The demo seed inserts the first team with an explicit id, which leaves the
-- team id sequence behind. Restoring it keeps "create team" working on any
-- database initialized from that seed.
SELECT setval(pg_get_serial_sequence('team', 'id'), GREATEST(COALESCE((SELECT MAX(id) FROM team), 1), 1));
