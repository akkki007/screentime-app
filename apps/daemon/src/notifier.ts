import { APP_ID } from '@screentime/shared';
import { quoteString } from './gvariant';
import { type Runner, bunRunner } from './runner';

export interface Notifier {
  notify(title: string, body: string): Promise<void>;
}

/**
 * Desktop notifications over `org.freedesktop.Notifications`, which every
 * desktop implements, via `gdbus` (no D-Bus library in the daemon; see ADR 6).
 */
export class DbusNotifier implements Notifier {
  constructor(private readonly runner: Runner = bunRunner) {}

  async notify(title: string, body: string): Promise<void> {
    // Notify(app_name, replaces_id, icon, summary, body, actions, hints, expire_timeout)
    const { ok } = await this.runner.run([
      'gdbus',
      'call',
      '--session',
      '--dest',
      'org.freedesktop.Notifications',
      '--object-path',
      '/org/freedesktop/Notifications',
      '--method',
      'org.freedesktop.Notifications.Notify',
      'Screentime',
      '0',
      quoteString(APP_ID),
      quoteString(title),
      quoteString(body),
      '[]',
      '{}',
      '8000',
    ]);
    // A missing notification daemon must never take tracking down.
    if (!ok)
      console.error('[notifier] could not send a notification (is a notification daemon running?)');
  }
}
