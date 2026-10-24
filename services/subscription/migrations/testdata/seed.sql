-- Test data with known tokens. Applied by the test harness on top of the schema migrations, never in production.
INSERT INTO users (id, email) VALUES (1, 'seed@example.com');
INSERT INTO repositories (id, full_name, last_seen_tag) VALUES (1, 'golang/go', 'go1.25.3'), (2, 'rust-lang/rust', NULL);
INSERT INTO subscriptions (id, user_id, repository_id, token, is_confirmed) VALUES
    (1, 1, 1, '11111111-1111-4111-8111-111111111111', FALSE),
    (2, 1, 2, '22222222-2222-4222-8222-222222222222', TRUE);

SELECT setval(pg_get_serial_sequence('users', 'id'), 1);
SELECT setval(pg_get_serial_sequence('repositories', 'id'), 2);
SELECT setval(pg_get_serial_sequence('subscriptions', 'id'), 2);
