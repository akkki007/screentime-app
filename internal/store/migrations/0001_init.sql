-- Initial schema. See docs/architecture.md#data-model.
CREATE TABLE apps (
  id INTEGER PRIMARY KEY,
  app_id TEXT UNIQUE NOT NULL,
  name TEXT,
  icon TEXT,
  category_id INTEGER REFERENCES categories(id)
);

CREATE TABLE categories (
  id INTEGER PRIMARY KEY,
  name TEXT UNIQUE,
  color TEXT,
  productive INTEGER
);

CREATE TABLE sessions (
  id INTEGER PRIMARY KEY,
  app_id INTEGER NOT NULL REFERENCES apps(id),
  title TEXT,
  start_ts INTEGER NOT NULL,
  end_ts INTEGER NOT NULL,
  source TEXT NOT NULL -- 'desktop' | 'browser'
);

CREATE TABLE web_sessions (
  id INTEGER PRIMARY KEY,
  domain TEXT NOT NULL,
  start_ts INTEGER,
  end_ts INTEGER
);

CREATE TABLE limits (
  id INTEGER PRIMARY KEY,
  target_type TEXT, -- 'app' | 'category' | 'domain'
  target TEXT,
  daily_ms INTEGER,
  schedule TEXT,
  action TEXT -- 'notify' | 'overlay' | 'block'
);

CREATE INDEX idx_sessions_start ON sessions(start_ts);
