-- Test data. Applied by the test harness and by the smoke test on top of the schema migrations, never in production.
INSERT INTO repositories (id, full_name, last_seen_tag) VALUES (1, 'golang/go', 'go1.25.3');

SELECT setval(pg_get_serial_sequence('repositories', 'id'), 1);
