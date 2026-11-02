import GLib from 'gi://GLib';
/**
 * What is playing, for the island's "Now playing" card: Spotify, YouTube or
 * any other site playing in a browser, or any other media player. Every one
 * of them publishes itself over MPRIS on the session bus, so this reads the
 * track and sends play/pause, next and previous without touching the apps.
 *
 * Nothing leaves the computer: cover art is shown only when the player gives a
 * local file (browsers do); a web address (Spotify's) is never fetched, and the
 * player's own icon is shown instead.
 */
import Gio from 'gi://Gio';

import { pickPlayer } from './mediaRules.js';

const PREFIX = 'org.mpris.MediaPlayer2.';
const PATH = '/org/mpris/MediaPlayer2';

const PlayerIface = `
<node>
  <interface name="org.mpris.MediaPlayer2.Player">
    <method name="PlayPause"/>
    <method name="Next"/>
    <method name="Previous"/>
    <property name="PlaybackStatus" type="s" access="read"/>
    <property name="Metadata" type="a{sv}" access="read"/>
    <property name="CanGoNext" type="b" access="read"/>
    <property name="CanGoPrevious" type="b" access="read"/>
    <property name="CanPlay" type="b" access="read"/>
    <property name="CanPause" type="b" access="read"/>
  </interface>
</node>`;

const RootIface = `
<node>
  <interface name="org.mpris.MediaPlayer2">
    <property name="Identity" type="s" access="read"/>
    <property name="DesktopEntry" type="s" access="read"/>
  </interface>
</node>`;

const PlayerProxy = Gio.DBusProxy.makeProxyWrapper(PlayerIface);
const RootProxy = Gio.DBusProxy.makeProxyWrapper(RootIface);

/** A list of artists, or one, as one string. */
function joined(v) {
  if (Array.isArray(v)) return v.filter(Boolean).join(', ');
  return typeof v === 'string' ? v : '';
}

export class Media {
  /** @param {() => void} onChange called (coalesced) when anything about what plays changes */
  constructor(onChange) {
    this._onChange = onChange;
    this._players = new Map(); // bus name -> {player, root, ids, changedAt}
    this._cancel = null;
    this._ownerId = 0;
    this._queued = 0;
  }

  start() {
    this._cancel = new Gio.Cancellable();
    this._ownerId = Gio.DBus.session.signal_subscribe(
      'org.freedesktop.DBus',
      'org.freedesktop.DBus',
      'NameOwnerChanged',
      '/org/freedesktop/DBus',
      PREFIX.slice(0, -1),
      Gio.DBusSignalFlags.MATCH_ARG0_NAMESPACE,
      (_c, _s, _p, _i, _sig, params) => {
        const [name, , owner] = params.deepUnpack();
        if (!name.startsWith(PREFIX)) return;
        if (owner) this._add(name);
        else this._remove(name);
      },
    );
    Gio.DBus.session.call(
      'org.freedesktop.DBus',
      '/org/freedesktop/DBus',
      'org.freedesktop.DBus',
      'ListNames',
      null,
      new GLib.VariantType('(as)'),
      Gio.DBusCallFlags.NONE,
      -1,
      this._cancel,
      (conn, res) => {
        try {
          const [names] = conn.call_finish(res).deepUnpack();
          for (const n of names) if (n.startsWith(PREFIX)) this._add(n);
        } catch (e) {
          if (!e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED))
            console.error(`Screentime island: media: ${e.message}`);
        }
      },
    );
  }

  stop() {
    this._cancel?.cancel();
    this._cancel = null;
    if (this._ownerId) Gio.DBus.session.signal_unsubscribe(this._ownerId);
    this._ownerId = 0;
    for (const name of [...this._players.keys()]) this._remove(name);
    if (this._queued) GLib.source_remove(this._queued);
    this._queued = 0;
  }

  /** Every player with something loaded, playing ones first, then the most recent. */
  all() {
    return [...this._players.entries()]
      .map(([name, e]) => this._describe(name, e))
      .filter((p) => p && (p.title || p.status === 'Playing'))
      .sort(
        (a, b) => (b.status === 'Playing') - (a.status === 'Playing') || b.changedAt - a.changedAt,
      );
  }

  /** What to show, or null when nothing plays or has played. */
  current() {
    const list = [...this._players.entries()].map(([name, e]) => this._describe(name, e));
    return pickPlayer(list.filter(Boolean));
  }

  playPause(name) {
    this._players.get(name)?.player.PlayPauseRemote(() => {});
  }

  next(name) {
    this._players.get(name)?.player.NextRemote(() => {});
  }

  previous(name) {
    this._players.get(name)?.player.PreviousRemote(() => {});
  }

  _add(name) {
    if (this._players.has(name)) return;
    const entry = { player: null, root: null, ids: [], changedAt: Date.now() };
    this._players.set(name, entry);
    // Proxies load their properties asynchronously; nothing blocks the Shell.
    new PlayerProxy(
      Gio.DBus.session,
      name,
      PATH,
      (proxy, error) => {
        if (error || this._players.get(name) !== entry) return;
        entry.player = proxy;
        entry.ids.push(
          proxy.connect('g-properties-changed', () => {
            entry.changedAt = Date.now();
            this._changed();
          }),
        );
        this._changed();
      },
      this._cancel,
    );
    new RootProxy(
      Gio.DBus.session,
      name,
      PATH,
      (proxy, error) => {
        if (error || this._players.get(name) !== entry) return;
        entry.root = proxy;
        this._changed();
      },
      this._cancel,
    );
  }

  _remove(name) {
    const e = this._players.get(name);
    if (!e) return;
    for (const id of e.ids) e.player?.disconnect(id);
    this._players.delete(name);
    this._changed();
  }

  _describe(name, e) {
    if (!e.player) return null;
    const meta = e.player.get_cached_property('Metadata')?.recursiveUnpack() ?? {};
    const art = typeof meta['mpris:artUrl'] === 'string' ? meta['mpris:artUrl'] : '';
    let artPath = '';
    if (art.startsWith('file://')) {
      try {
        artPath = GLib.filename_from_uri(art)[0];
      } catch (_e) {
        artPath = '';
      }
    }
    const identity = e.root?.Identity ?? '';
    return {
      name,
      identity: identity || name.slice(PREFIX.length).split('.')[0],
      desktopEntry: e.root?.DesktopEntry ?? '',
      title: joined(meta['xesam:title']),
      artist: joined(meta['xesam:artist']),
      album: joined(meta['xesam:album']),
      artPath,
      status: e.player.PlaybackStatus ?? 'Stopped',
      canNext: e.player.CanGoNext ?? false,
      canPrevious: e.player.CanGoPrevious ?? false,
      changedAt: e.changedAt,
    };
  }

  /** Several properties often change at once (a new track): tell the island once. */
  _changed() {
    if (this._queued) return;
    this._queued = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 60, () => {
      this._queued = 0;
      this._onChange();
      return GLib.SOURCE_REMOVE;
    });
  }
}
