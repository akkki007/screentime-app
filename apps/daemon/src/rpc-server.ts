import type { Database } from 'bun:sqlite';
import { chmodSync, mkdirSync, rmSync } from 'node:fs';
import { dirname } from 'node:path';
import {
  BrowserActiveTabSchema,
  IPC_VERSION,
  RpcMethods,
  type RpcNotifications,
  runtimeSocketPath,
} from '@screentime/shared';
import type { BrowserActiveTab } from '@screentime/shared';
import { ZodError, type z } from 'zod';

type JsonRpcRequest = {
  jsonrpc: '2.0';
  id: number | string | null;
  method: string;
  params?: unknown;
};

type JsonRpcResponse = {
  jsonrpc: '2.0';
  id: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
};

export type MethodHandlers = {
  [K in keyof typeof RpcMethods]?: (params: never) => unknown | Promise<unknown>;
} & {
  /** Fire-and-forget notification from the browser extension's native messaging host. */
  'browser.activeTab'?: (params: BrowserActiveTab) => void;
};

type NotificationName = keyof typeof RpcNotifications;

/** A line longer than this is a misbehaving client; drop it rather than buffer forever. */
const MAX_LINE_BYTES = 1_000_000;

type ClientSocket = {
  write: (data: string) => unknown;
  end: () => unknown;
  data: { buffer: string };
};

/**
 * Whether a daemon is already listening on `path`. Starting a second one would
 * unlink the first's socket and both would write the same database, double
 * counting every session, so the daemon checks this before binding.
 */
export async function isSocketLive(path = runtimeSocketPath()): Promise<boolean> {
  try {
    const socket = await Bun.connect({ unix: path, socket: { data() {} } });
    socket.end();
    return true;
  } catch {
    return false;
  }
}

export type RpcServer = {
  /** Sends a daemon → UI notification to every connected client. */
  broadcast: <N extends NotificationName>(
    method: N,
    params: z.infer<(typeof RpcNotifications)[N]>,
  ) => void;
  stop: () => void;
};

/**
 * JSON-RPC 2.0 server over a Unix socket at
 * $XDG_RUNTIME_DIR/screentime/daemon.sock (mode 0600). One newline-delimited
 * JSON message per request/response, matching docs/architecture.md#ipc-contracts.
 */
export function startRpcServer(
  _db: Database,
  handlers: MethodHandlers,
  path = runtimeSocketPath(),
): RpcServer {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  try {
    rmSync(path);
  } catch {
    // socket didn't exist yet
  }

  const clients = new Set<ClientSocket>();

  const server = Bun.listen<{ buffer: string }>({
    unix: path,
    socket: {
      open(socket) {
        socket.data = { buffer: '' };
        clients.add(socket);
      },
      close(socket) {
        clients.delete(socket);
      },
      error(socket) {
        clients.delete(socket);
      },
      data(socket, chunk) {
        socket.data.buffer += chunk.toString('utf8');

        let newlineIndex: number;
        // biome-ignore lint/suspicious/noAssignInExpressions: streaming line parser
        while ((newlineIndex = socket.data.buffer.indexOf('\n')) !== -1) {
          const line = socket.data.buffer.slice(0, newlineIndex);
          socket.data.buffer = socket.data.buffer.slice(newlineIndex + 1);
          if (line.trim()) {
            handleLine(socket, line, handlers).catch((err) => {
              console.error('[rpc] unhandled error:', err);
            });
          }
        }

        if (socket.data.buffer.length > MAX_LINE_BYTES) {
          console.error('[rpc] client sent an oversized message; disconnecting');
          clients.delete(socket);
          socket.end();
        }
      },
    },
  });

  chmodSync(path, 0o600);

  return {
    broadcast(method, params) {
      const line = `${JSON.stringify({ jsonrpc: '2.0', method, params })}\n`;
      for (const client of clients) client.write(line);
    },
    stop() {
      server.stop(true);
      try {
        rmSync(path);
      } catch {
        // already gone
      }
    },
  };
}

async function handleLine(
  socket: { write: (data: string) => unknown },
  line: string,
  handlers: MethodHandlers,
): Promise<void> {
  let req: JsonRpcRequest;
  try {
    const parsed = JSON.parse(line);
    if (typeof parsed !== 'object' || parsed === null || typeof parsed.method !== 'string') return;
    req = parsed;
  } catch {
    return;
  }

  const isNotification = req.id === undefined || req.id === null;
  const respond = (res: Omit<JsonRpcResponse, 'jsonrpc' | 'id'>) => {
    if (isNotification) return;
    const message: JsonRpcResponse = { jsonrpc: '2.0', id: req.id, ...res };
    socket.write(`${JSON.stringify(message)}\n`);
  };

  if (req.method === 'browser.activeTab') {
    const parsed = BrowserActiveTabSchema.safeParse(req.params);
    if (parsed.success) handlers['browser.activeTab']?.(parsed.data);
    else
      console.error('[rpc] ignoring malformed browser.activeTab:', parsed.error.issues[0]?.message);
    return;
  }

  if (req.method === 'version') {
    const { major } = (req.params as { major?: number } | undefined) ?? {};
    if (major !== IPC_VERSION) {
      respond({ error: { code: -32000, message: `unsupported major version ${major}` } });
      return;
    }
    respond({ result: { major: IPC_VERSION } });
    return;
  }

  const methodDef = RpcMethods[req.method as keyof typeof RpcMethods];
  const handler = handlers[req.method as keyof typeof RpcMethods] as
    | ((params: unknown) => unknown | Promise<unknown>)
    | undefined;
  if (!methodDef || !handler) {
    respond({ error: { code: -32601, message: `method not found: ${req.method}` } });
    return;
  }

  try {
    const params = methodDef.request.parse(req.params);
    const result = await handler(params);
    respond({ result: methodDef.response.parse(result) });
  } catch (err) {
    respond({ error: { code: -32603, message: describeError(err) } });
  }
}

/** Zod errors become "field: reason" so a UI can show them; other errors pass through. */
function describeError(err: unknown): string {
  if (err instanceof ZodError) {
    return err.issues
      .map((issue) => `${issue.path.join('.') || 'params'}: ${issue.message}`)
      .join('; ');
  }
  return err instanceof Error ? err.message : String(err);
}
