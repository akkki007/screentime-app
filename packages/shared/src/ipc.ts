/**
 * JSON-RPC 2.0 payload schemas shared between the daemon (server) and the
 * desktop UI (client), talking over $XDG_RUNTIME_DIR/screentime/daemon.sock.
 * See docs/architecture.md#ipc-contracts for the full contract table.
 */
import { z } from 'zod';
import { SettingsPatchSchema, SettingsSchema } from './settings';

export const GroupBySchema = z.enum(['app', 'category', 'hour', 'day']);
export type GroupBy = z.infer<typeof GroupBySchema>;

export const UsageSummaryRequestSchema = z.object({
  from: z.number().int(),
  to: z.number().int(),
  groupBy: GroupBySchema,
});
export type UsageSummaryRequest = z.infer<typeof UsageSummaryRequestSchema>;

export const UsageSummaryRowSchema = z.object({
  key: z.string(),
  ms: z.number().int().nonnegative(),
});
export type UsageSummaryRow = z.infer<typeof UsageSummaryRowSchema>;

export const UsageTimelineRequestSchema = z.object({
  date: z.string(), // YYYY-MM-DD, local
});
export type UsageTimelineRequest = z.infer<typeof UsageTimelineRequestSchema>;

export const SessionSchema = z.object({
  id: z.number().int(),
  appId: z.string(),
  title: z.string().optional(),
  startTs: z.number().int(),
  endTs: z.number().int(),
  source: z.enum(['desktop', 'browser']),
});
export type Session = z.infer<typeof SessionSchema>;

export const UsageWebRequestSchema = z.object({
  from: z.number().int(),
  to: z.number().int(),
});
export type UsageWebRequest = z.infer<typeof UsageWebRequestSchema>;

export const CategorySchema = z.object({
  id: z.number().int(),
  name: z.string(),
  color: z.string().nullable(),
  /** 1 productive, 0 distracting, null neutral. */
  productive: z.number().int().nullable(),
});
export type Category = z.infer<typeof CategorySchema>;

export const AppInfoSchema = z.object({
  appId: z.string(),
  name: z.string().nullable(),
  icon: z.string().nullable(),
  categoryId: z.number().int().nullable(),
});
export type AppInfo = z.infer<typeof AppInfoSchema>;

export const SetAppCategoryRequestSchema = z.object({
  appId: z.string(),
  categoryId: z.number().int().nullable(),
});

export const TrackerStatusSchema = z.object({
  paused: z.boolean(),
  resumeAt: z.number().int().nullable(),
  idle: z.boolean(),
  currentAppId: z.string().nullable(),
  /** Unix ms when the current app took focus, if tracked. */
  since: z.number().int().nullable(),
  focusMode: z.object({ active: z.boolean(), until: z.number().int().nullable() }),
});
export type TrackerStatus = z.infer<typeof TrackerStatusSchema>;

export const FocusModeStartRequestSchema = z.object({
  minutes: z
    .number()
    .int()
    .positive()
    .max(24 * 60),
});

export const DataExportRequestSchema = z.object({
  format: z.enum(['csv', 'json']),
  from: z.number().int().optional(),
  to: z.number().int().optional(),
});
export const DataExportResponseSchema = z.object({
  filename: z.string(),
  content: z.string(),
});
export type DataExportResponse = z.infer<typeof DataExportResponseSchema>;

export const DataWipeRequestSchema = z.object({
  /** Also reset settings, limits and category overrides. */
  everything: z.boolean().default(false),
});

export const LimitTargetTypeSchema = z.enum(['app', 'category', 'domain']);
export const LimitActionSchema = z.enum(['notify', 'overlay', 'block']);

export const LimitSchema = z.object({
  id: z.number().int().optional(),
  targetType: LimitTargetTypeSchema,
  target: z.string(),
  dailyMs: z.number().int().positive(),
  /**
   * Optional daily window ("HH:MM-HH:MM", may wrap midnight) during which the
   * limit is enforced. Usage still counts for the whole day.
   */
  schedule: z
    .string()
    .regex(/^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$/, 'expected HH:MM-HH:MM')
    .optional(),
  action: LimitActionSchema,
});
export type Limit = z.infer<typeof LimitSchema>;

export const TrackerPauseRequestSchema = z.object({
  minutes: z.number().int().positive(),
});
export type TrackerPauseRequest = z.infer<typeof TrackerPauseRequestSchema>;

export const TrackerPauseResponseSchema = z.object({
  resumeAt: z.number().int(),
});
export type TrackerPauseResponse = z.infer<typeof TrackerPauseResponseSchema>;

export const FocusEventSchema = z.object({
  appId: z.string(),
  since: z.number().int(),
});
export type FocusEvent = z.infer<typeof FocusEventSchema>;

export const ReminderEventSchema = z.object({
  kind: z.enum(['break', 'downtime', 'focus']),
  message: z.string(),
});
export type ReminderEvent = z.infer<typeof ReminderEventSchema>;

export const LimitHitEventSchema = z.object({
  limitId: z.number().int(),
  action: LimitActionSchema,
});
export type LimitHitEvent = z.infer<typeof LimitHitEventSchema>;

export const VersionHandshakeSchema = z.object({
  major: z.number().int(),
});
export type VersionHandshake = z.infer<typeof VersionHandshakeSchema>;

/** UI → daemon RPC methods and their request/response schemas. */
export const RpcMethods = {
  'usage.summary': {
    request: UsageSummaryRequestSchema,
    response: z.array(UsageSummaryRowSchema),
  },
  'usage.timeline': {
    request: UsageTimelineRequestSchema,
    response: z.array(SessionSchema),
  },
  'limits.list': {
    request: z.void(),
    response: z.array(LimitSchema),
  },
  'limits.set': {
    request: LimitSchema,
    response: LimitSchema,
  },
  'limits.delete': {
    request: z.object({ id: z.number().int() }),
    response: z.object({ ok: z.boolean() }),
  },
  'settings.get': {
    request: z.void(),
    response: SettingsSchema,
  },
  'settings.set': {
    request: SettingsPatchSchema,
    response: SettingsSchema,
  },
  'tracker.pause': {
    request: TrackerPauseRequestSchema,
    response: TrackerPauseResponseSchema,
  },
  'tracker.resume': {
    request: z.void(),
    response: z.object({ ok: z.boolean() }),
  },
  'tracker.status': {
    request: z.void(),
    response: TrackerStatusSchema,
  },
  'usage.web': {
    request: UsageWebRequestSchema,
    response: z.array(UsageSummaryRowSchema),
  },
  'apps.list': {
    request: z.void(),
    response: z.array(AppInfoSchema),
  },
  'apps.setCategory': {
    request: SetAppCategoryRequestSchema,
    response: z.object({ ok: z.boolean() }),
  },
  'categories.list': {
    request: z.void(),
    response: z.array(CategorySchema),
  },
  'focus.start': {
    request: FocusModeStartRequestSchema,
    response: z.object({ until: z.number().int() }),
  },
  'focus.stop': {
    request: z.void(),
    response: z.object({ ok: z.boolean() }),
  },
  'data.export': {
    request: DataExportRequestSchema,
    response: DataExportResponseSchema,
  },
  'data.wipe': {
    request: DataWipeRequestSchema,
    response: z.object({ ok: z.boolean() }),
  },
} as const;

/** daemon → UI notifications (no response expected). */
export const RpcNotifications = {
  'event.focus': FocusEventSchema,
  'event.limitHit': LimitHitEventSchema,
  'event.reminder': ReminderEventSchema,
  'event.status': TrackerStatusSchema,
} as const;

/** browser extension (via the native messaging host) → daemon notification. */
export const BrowserActiveTabSchema = z.object({
  domain: z.string().max(253).optional(),
  active: z.boolean(),
});
export type BrowserActiveTab = z.infer<typeof BrowserActiveTabSchema>;
