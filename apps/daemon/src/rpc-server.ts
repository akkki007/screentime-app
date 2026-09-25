import type { Database } from 'bun:sqlite';
import { chmodSync, mkdirSync, rmSync } from 'node:fs';
import { dirname } from 'node:path';
import {
  BrowserActiveTabSchema,
  IPC_VERSION,
  RpcMethods,
  runtimeSocketPath,
} from '@screentime/shared';
import type { BrowserActiveTab } from '@screentime/shared';

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
  [K in keyof typeof RpcMethods]?: (params: unknown) => unknown | Promise<unknown>;
} & {
  /** Fire-and-forget notification from the browser extension's native messaging host. */
  'browser.activeTab'?: (params: BrowserActiveTab) => void;
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
) {
  mkdirSync(dirname(path), { recursive: true });
  try {
    rmSync(path);
  } catch {
    // socket didn't exist yet
  }

  const server = Bun.listen<{ buffer: string }>({
    unix: path,
    socket: {
      open(socket) {
        socket.data = { buffer: '' };
      },
      data(socket, chunk) {
        socket.data.buffer += chunk.toString('utf8');
        let newlineIndex: number;
        // biome-ignore lint/suspicious/noAssignInExpressions: streaming line parser
        while ((newlineIndex = socket.data.buffer.indexOf('\n')) !== -1) {
          const line = socket.data.buffer.slice(0, newlineIndex);
          socket.data.buffer = socket.data.buffer.slice(newlineIndex + 1);
          if (line.trim()) handleLine(socket, line, handlers);
        }
      },
    },
  });

  chmodSync(path, 0o600);
  return server;
}

async function handleLine(
  socket: { write: (data: string) => void },
  line: string,
  handlers: MethodHandlers,
): Promise<void> {
  let req: JsonRpcRequest;
  try {
    req = JSON.parse(line);
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
    handlers['browser.activeTab']?.(BrowserActiveTabSchema.parse(req.params));
    return;
  }

  if (req.method === 'version') {
    const { major } = (req.params as { major?: number }) ?? {};
    if (major !== IPC_VERSION) {
      respond({ error: { code: -32000, message: `unsupported major version ${major}` } });
      return;
    }
    respond({ result: { major: IPC_VERSION } });
    return;
  }

  const methodDef = RpcMethods[req.method as keyof typeof RpcMethods];
  const handler = handlers[req.method as keyof typeof RpcMethods];
  if (!methodDef || !handler) {
    respond({ error: { code: -32601, message: `method not found: ${req.method}` } });
    return;
  }

  try {
    const params = methodDef.request.parse(req.params);
    const result = await handler(params);
    respond({ result: methodDef.response.parse(result) });
  } catch (err) {
    respond({ error: { code: -32603, message: err instanceof Error ? err.message : String(err) } });
  }
}
