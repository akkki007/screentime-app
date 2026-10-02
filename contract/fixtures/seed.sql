-- Seed data for the contract fixtures. Applied after migrations, before the
-- daemon starts. Fixtures run with TZ=Asia/Kolkata (+05:30, no DST) and the
-- clock frozen at 1790762400000 = Wed 2026-09-30 15:30 local; times below are local.
-- Every app has a name so no .desktop lookup (which depends on the host) runs.
-- Generated values are plain literals so any implementation can apply them.

INSERT INTO apps (id, app_id, name, icon, category_id) VALUES
  (1, 'code', 'Visual Studio Code', 'vscode', (SELECT id FROM categories WHERE name='Development')),
  (2, 'org.mozilla.firefox', 'Firefox', 'firefox', (SELECT id FROM categories WHERE name='Browsing')),
  (3, 'org.gnome.Ptyxis', 'Terminal', NULL, (SELECT id FROM categories WHERE name='Development')),
  (4, 'com.spotify.Client', 'Spotify', NULL, (SELECT id FROM categories WHERE name='Entertainment')),
  (5, 'org.telegram.desktop', 'Telegram', NULL, (SELECT id FROM categories WHERE name='Communication')),
  (6, 'com.example.Unknown', 'Unknown Tool', NULL, NULL),
  (7, 'org.gnome.Nautilus', 'Files', NULL, (SELECT id FROM categories WHERE name='Productivity'));

INSERT INTO sessions (app_id, title, start_ts, end_ts, source) VALUES
  (1, NULL, 1790051400000, 1790055000000, 'desktop'), -- Sep 22 10:00-11:00 (Sep 22: outside a 7-day window)
  (1, NULL, 1790483400000, 1790490600000, 'desktop'), -- Sep 27 10:00-12:00 (Sep 27)
  (1, NULL, 1790652600000, 1790661600000, 'desktop'), -- Sep 29 09:00-11:30 (Sep 29)
  (2, 'Docs', 1790661600000, 1790664300000, 'desktop'), -- Sep 29 11:30-12:15 (Sep 29, with a title)
  (4, NULL, 1790704800000, 1790709300000, 'desktop'), -- Sep 29 23:30-00:45 (crosses local midnight Sep 29 -> 30)
  (3, NULL, 1790735400000, 1790736600000, 'desktop'), -- Sep 30 08:00-08:20 (Sep 30 (today))
  (1, NULL, 1790739000000, 1790744400000, 'desktop'), -- Sep 30 09:00-10:30
  (2, NULL, 1790744400000, 1790746200000, 'browser'), -- Sep 30 10:30-11:00
  (5, NULL, 1790746200000, 1790746800000, 'desktop'), -- Sep 30 11:00-11:10
  (6, NULL, 1790749800000, 1790750100000, 'desktop'), -- Sep 30 12:00-12:05 (no category)
  (7, NULL, 1790753400000, 1790755200000, 'desktop'), -- Sep 30 13:00-13:30 (user override: Productivity, not the default Utilities)
  (1, NULL, 1790757000000, 1790761800000, 'desktop'); -- Sep 30 14:00-15:20 (ends 10 min before "now")

INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES
  ('youtube.com', 1790661600000, 1790664300000), -- Sep 29 11:30-12:15
  ('github.com', 1790744400000, 1790745600000), -- Sep 30 10:30-10:50
  ('youtube.com', 1790745600000, 1790746200000); -- Sep 30 10:50-11:00

INSERT INTO limits (id, target_type, target, daily_ms, schedule, action) VALUES
  (1, 'app', 'com.spotify.Client', 1800000, NULL, 'notify'),
  (2, 'category', 'Entertainment', 3600000, '09:00-18:00', 'overlay'),
  (3, 'domain', 'youtube.com', 2700000, NULL, 'notify');

-- Settings are one JSON value per key; unset keys use the defaults.
INSERT INTO settings (key, value) VALUES
  ('idleThresholdMinutes', '5'),
  ('onboardingDone', 'true');
