import { z } from 'zod';

const ClockTime = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, 'expected HH:MM');

/**
 * User-editable settings, persisted by the daemon in the `settings` table
 * (one JSON value per key). Privacy-sensitive options default to off.
 */
export const SettingsSchema = z.object({
  /** Window titles often contain private data, so they are opt-in. */
  captureTitles: z.boolean().default(false),
  /** No input for this long closes the open session. */
  idleThresholdMinutes: z.number().int().min(1).max(60).default(3),

  breakRemindersEnabled: z.boolean().default(false),
  /** Remind after this much continuous active time. */
  breakEveryMinutes: z.number().int().min(5).max(240).default(50),
  /** Being away this long counts as a real break. */
  breakLengthMinutes: z.number().int().min(1).max(60).default(5),

  downtimeEnabled: z.boolean().default(false),
  downtimeStart: ClockTime.default('22:00'),
  downtimeEnd: ClockTime.default('07:00'),

  onboardingDone: z.boolean().default(false),

  /** Show the notch at the top of the screen (ADR 14). Off by default: many people already have one. */
  notchEnabled: z.boolean().default(false),

  /**
   * The optional daily planner and AI categorisation. Off by default: while it
   * is off the daemon starts nothing and reads nothing for it.
   */
  plannerEnabled: z.boolean().default(false),
  /** "local" runs a llama-server sidecar; "remote" calls an OpenAI-compatible endpoint with the user's own key. */
  plannerProvider: z.enum(['local', 'remote']).default('local'),
  /** Remote base URL, e.g. https://api.openai.com/v1. https is required unless the host is loopback. */
  plannerEndpoint: z
    .string()
    .max(200)
    .regex(/^(https?:\/\/[^\s]+)?$/, 'expected an http(s) URL')
    .default(''),
  /** Remote model name. */
  plannerModel: z.string().max(100).default(''),
  /** The llama-server binary; empty means look in PATH. */
  plannerServerPath: z.string().max(300).default(''),
  /** The .gguf model file the user downloaded. */
  plannerModelPath: z.string().max(300).default(''),
  /** The local sidecar stops after this long without a request. */
  plannerIdleMinutes: z.number().int().min(1).max(60).default(5),
});
export type Settings = z.infer<typeof SettingsSchema>;

/** `settings.set` takes any subset of settings; unknown keys are rejected. */
export const SettingsPatchSchema = SettingsSchema.partial().strict();
export type SettingsPatch = z.infer<typeof SettingsPatchSchema>;

export const DEFAULT_SETTINGS: Settings = SettingsSchema.parse({});
