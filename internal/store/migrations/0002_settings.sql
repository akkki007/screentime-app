-- User settings, one JSON-encoded value per key. See packages/shared/src/settings.ts.
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- At most one limit per target and kind of scope.
CREATE UNIQUE INDEX idx_limits_target ON limits(target_type, target);

CREATE INDEX idx_web_sessions_start ON web_sessions(start_ts);
