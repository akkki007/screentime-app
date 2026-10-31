import GLib from 'gi://GLib';
/**
 * A small JSON-RPC client for the Screentime daemon, for code running inside
 * GNOME Shell (the notch). One line of JSON per message over the daemon's Unix
 * socket, like every other client (docs/architecture.md#ipc-contracts).
 *
 * Like the UI shell and the browser host it checks, before connecting, that
 * the socket and its directory belong to the current user (S3).
 */
import Gio from 'gi://Gio';

const MAJOR = 1;
const RETRY_SECONDS = 2;
const CALL_TIMEOUT_SECONDS = 10;

const ATTRS = 'standard::type,unix::uid,unix::mode';
const NOFOLLOW = Gio.FileQueryInfoFlags.NOFOLLOW_SYMLINKS;

function socketPath() {
  return GLib.build_filenamev([GLib.get_user_runtime_dir(), 'screentime', 'daemon.sock']);
}

/** Why the socket can't be trusted, or '' when it can. */
function ownershipProblem(path) {
  try {
    const me = Gio.File.new_for_path('/proc/self')
      .query_info('unix::uid', 0, null)
      .get_attribute_uint32('unix::uid');
    const dir = Gio.File.new_for_path(GLib.path_get_dirname(path)).query_info(
      ATTRS,
      NOFOLLOW,
      null,
    );
    if (dir.get_file_type() !== Gio.FileType.DIRECTORY) return 'its directory is not a directory';
    if (dir.get_attribute_uint32('unix::uid') !== me) return 'its directory is not yours';
    if (dir.get_attribute_uint32('unix::mode') & 0o077)
      return 'its directory is open to other users';
    const sock = Gio.File.new_for_path(path).query_info(ATTRS, NOFOLLOW, null);
    if (sock.get_file_type() !== Gio.FileType.SPECIAL) return 'it is not a socket';
    if (sock.get_attribute_uint32('unix::uid') !== me) return 'it is not yours';
  } catch (e) {
    return e.message;
  }
  return '';
}

export class DaemonClient {
  /** @param {{onNotification: (method: string, params: any) => void, onState: (connected: boolean) => void}} handlers */
  constructor({ onNotification, onState }) {
    this._onNotification = onNotification;
    this._onState = onState;
    this._conn = null;
    this._out = null;
    this._in = null;
    this._nextId = 1;
    this._pending = new Map();
    this._retryId = 0;
    this._stopped = true;
    this._cancel = null;
  }

  get connected() {
    return this._conn !== null;
  }

  start() {
    if (!this._stopped) return;
    this._stopped = false;
    this._connect();
  }

  stop() {
    this._stopped = true;
    if (this._retryId) {
      GLib.source_remove(this._retryId);
      this._retryId = 0;
    }
    this._cancel?.cancel();
    this._drop();
  }

  /** @returns {Promise<any>} the result, or rejects with the daemon's error. */
  call(method, params) {
    return new Promise((resolve, reject) => {
      if (!this._out) {
        reject(new Error('Screentime is not running'));
        return;
      }
      const id = this._nextId++;
      const timer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, CALL_TIMEOUT_SECONDS, () => {
        this._pending.delete(id);
        reject(new Error(`${method} timed out`));
        return GLib.SOURCE_REMOVE;
      });
      this._pending.set(id, { resolve, reject, timer });
      const msg = { jsonrpc: '2.0', id, method };
      if (params !== undefined) msg.params = params;
      this._write(msg);
    });
  }

  _write(msg) {
    try {
      this._out.write_all(`${JSON.stringify(msg)}\n`, null);
      this._out.flush(null);
    } catch (e) {
      this._drop();
      this._scheduleRetry();
    }
  }

  _connect() {
    if (this._stopped) return;
    const path = socketPath();
    if (ownershipProblem(path)) {
      this._scheduleRetry();
      return;
    }
    this._cancel = new Gio.Cancellable();
    new Gio.SocketClient().connect_async(
      new Gio.UnixSocketAddress({ path }),
      this._cancel,
      (client, res) => {
        let conn;
        try {
          conn = client.connect_finish(res);
        } catch (e) {
          this._scheduleRetry();
          return;
        }
        if (this._stopped) {
          conn.close(null);
          return;
        }
        this._conn = conn;
        this._out = conn.get_output_stream();
        this._in = new Gio.DataInputStream({ base_stream: conn.get_input_stream() });
        this._read();
        // The handshake comes first; the notch counts as connected once it succeeds.
        this.call('version', { major: MAJOR }).then(
          () => this._onState(true),
          () => {
            this._drop();
            this._scheduleRetry();
          },
        );
      },
    );
  }

  _read() {
    const stream = this._in;
    stream?.read_line_async(GLib.PRIORITY_DEFAULT, this._cancel, (s, res) => {
      if (s !== this._in) return;
      let bytes;
      try {
        [bytes] = s.read_line_finish(res);
      } catch (e) {
        bytes = null;
      }
      if (bytes === null) {
        // The daemon went away (or restarted): wait for it to come back.
        this._drop();
        this._scheduleRetry();
        return;
      }
      this._handle(new TextDecoder().decode(bytes));
      this._read();
    });
  }

  _handle(line) {
    let msg;
    try {
      msg = JSON.parse(line);
    } catch (e) {
      return;
    }
    if (msg.id !== undefined && msg.id !== null) {
      const p = this._pending.get(msg.id);
      if (!p) return;
      this._pending.delete(msg.id);
      GLib.source_remove(p.timer);
      if (msg.error) p.reject(new Error(msg.error.message ?? 'error'));
      else p.resolve(msg.result);
    } else if (typeof msg.method === 'string') {
      this._onNotification(msg.method, msg.params);
    }
  }

  _drop() {
    const wasConnected = this._conn !== null;
    try {
      this._conn?.close(null);
    } catch (e) {
      // already closed
    }
    this._conn = this._out = this._in = null;
    for (const p of this._pending.values()) {
      GLib.source_remove(p.timer);
      p.reject(new Error('Screentime is not running'));
    }
    this._pending.clear();
    if (wasConnected) this._onState(false);
  }

  _scheduleRetry() {
    if (this._stopped || this._retryId) return;
    this._retryId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, RETRY_SECONDS, () => {
      this._retryId = 0;
      this._connect();
      return GLib.SOURCE_REMOVE;
    });
  }
}
