-- Who chose an app's category: 'auto' (built-in rules or .desktop Categories)
-- or 'user' (apps.setCategory). Auto rows may be re-evaluated; user rows never.
-- Existing rows are marked 'legacy' and resolved to auto/user by
-- apps.Backfill at daemon start, since that needs the Go substring rules.
ALTER TABLE apps ADD COLUMN category_source TEXT NOT NULL DEFAULT 'auto';
UPDATE apps SET category_source = 'legacy';
