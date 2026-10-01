import { APP_ID } from '@screentime/shared';
import { sessionBus } from 'dbus-next';

export interface Notifier {
  notify(title: string, body: string): Promise<void>;
}

/**
 * Desktop notifications over `org.freedesktop.Notifications`, which every
 * desktop implements. Connects lazily and reconnects after a failure.
 */
export class DbusNotifier implements Notifier {
  private bus: ReturnType<typeof sessionBus> | undefined;

  async notify(title: string, body: string): Promise<void> {
    try {
      this.bus ??= sessionBus();
      const obj = await this.bus.getProxyObject(
        'org.freedesktop.Notifications',
        '/org/freedesktop/Notifications',
      );
      const iface = obj.getInterface('org.freedesktop.Notifications');
      const notify = iface.Notify;
      if (!notify) throw new Error('org.freedesktop.Notifications has no Notify method');
      // (app_name, replaces_id, icon, summary, body, actions, hints, expire_timeout)
      await notify.call(iface, 'Screentime', 0, APP_ID, title, body, [], {}, 8_000);
    } catch (err) {
      // A missing notification daemon must never take tracking down.
      console.error('[notifier] failed to send notification:', err);
      this.bus?.disconnect();
      this.bus = undefined;
    }
  }
}
