/**
 * JSON-RPC 2.0 payload schemas shared between the daemon (server) and the
 * desktop UI (client), talking over $XDG_RUNTIME_DIR/screentime/daemon.sock.
 * See docs/architecture.md#ipc-contracts for the full contract table.
 */
import { z } from 'zod';

export const GroupBySchema = z.enum(['app', 'category', 'hour']);
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

export const LimitTargetTypeSchema = z.enum(['app', 'category', 'domain']);
export const LimitActionSchema = z.enum(['notify', 'overlay', 'block']);

export const LimitSchema = z.object({
  id: z.number().int().optional(),
  targetType: LimitTargetTypeSchema,
  target: z.string(),
  dailyMs: z.number().int().positive(),
  schedule: z.string().optional(),
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
    response: z.record(z.string(), z.unknown()),
  },
  'settings.set': {
    request: z.record(z.string(), z.unknown()),
    response: z.object({ ok: z.boolean() }),
  },
  'tracker.pause': {
    request: TrackerPauseRequestSchema,
    response: TrackerPauseResponseSchema,
  },
} as const;

/** daemon → UI notifications (no response expected). */
export const RpcNotifications = {
  'event.focus': FocusEventSchema,
  'event.limitHit': LimitHitEventSchema,
} as const;

/** browser extension (via the native messaging host) → daemon notification. */
export const BrowserActiveTabSchema = z.object({
  domain: z.string().optional(),
  active: z.boolean(),
});
export type BrowserActiveTab = z.infer<typeof BrowserActiveTabSchema>;
