-- Built-in categories. `productive`: 1 productive, 0 distracting, NULL neutral.
-- The app-to-category rules live in packages/shared/src/categories.ts.
INSERT OR IGNORE INTO categories (name, color, productive) VALUES
  ('Development', '#4f8cff', 1),
  ('Productivity', '#34c38f', 1),
  ('Communication', '#a78bfa', NULL),
  ('Social', '#f472b6', 0),
  ('Entertainment', '#fb923c', 0),
  ('Browsing', '#facc15', NULL),
  ('Utilities', '#94a3b8', NULL),
  ('Other', '#64748b', NULL);
