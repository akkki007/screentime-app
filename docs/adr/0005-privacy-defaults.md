# 5. Privacy defaults: titles off, hostnames only, private windows ignored

Date: 2026-10-01

## Status

Accepted

## Context

The README promises no telemetry and that window titles are opt-in. The first working tracker stored titles (the GNOME extension always sends them), which broke that promise. Titles routinely contain document names, chat contacts and page titles.

## Decision

- **Titles are dropped inside the tracker** unless the `captureTitles` setting is on, so the promise holds for every adapter, not just ones that remember to honour it. The setting defaults to off and can be changed at runtime.
- The browser extension sends **only the hostname** (never path, query or title), only for `http(s)` pages, and **ignores incognito/private windows**. Its manifest asks only for `tabs` and `nativeMessaging`, not `<all_urls>`.
- Turning titles off stops *new* titles being stored but does not delete old ones; deleting history is a separate, explicit action (Settings → Your data), which also VACUUMs and truncates the WAL so deleted rows are not left readable on disk.
- The CSV export neutralises spreadsheet formula injection from app and domain names.

## Consequences

- Per-page analytics are impossible by design.
- A user who enabled titles and then disabled them must delete history to remove what was stored; the UI says so.
