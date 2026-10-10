-- Early builds stored GNOME's `<id>.desktop` app IDs; the docs identify apps
-- without the suffix. Rename them unless that would collide with an existing row.
UPDATE apps
SET app_id = substr(app_id, 1, length(app_id) - 8)
WHERE app_id LIKE '%.desktop'
  AND NOT EXISTS (
    SELECT 1 FROM apps AS other
    WHERE other.app_id = substr(apps.app_id, 1, length(apps.app_id) - 8)
  );
