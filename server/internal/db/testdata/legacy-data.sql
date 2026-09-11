INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, definition_snapshot, output)
VALUES('legacy-running','lab/long-job','2026-09-10','RUNNING','2026-09-10 00:00:00','2026-09-10 00:00:00','{"id":"lab/long-job","jobType":"COMMAND"}','synthetic output');
INSERT INTO agent_tokens(token,label) VALUES('synthetic-legacy-token','fixture only');
INSERT INTO daily_runs(order_date,started_at) VALUES('2026-09-10','2026-09-10 00:00:00');
INSERT INTO design_sessions(id,actor,base_sha,path,created_at,last_touch) VALUES('legacy-draft','lab-user','baseline','synthetic/draft','2026-09-10 00:00:00','2026-09-10 00:00:00');
