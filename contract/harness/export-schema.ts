/**
 * Writes schema/rpc.schema.json: the JSON Schema of every RPC method,
 * notification and the browser host's message, generated from the Zod
 * schemas in packages/shared. Other implementations (the Go daemon) are
 * checked against this file; replay.test.ts fails if it is out of date.
 *
 *   bun run --cwd contract schema
 */
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import {
  BrowserActiveTabSchema,
  IPC_VERSION,
  RpcMethods,
  RpcNotifications,
  VersionHandshakeSchema,
} from '@screentime/shared';
import { type ZodTypeAny, ZodVoid } from 'zod';
import { zodToJsonSchema } from 'zod-to-json-schema';

export const SCHEMA_PATH = join(import.meta.dir, '..', 'schema', 'rpc.schema.json');

/** `null` means the method takes no params (omit `params`). */
function toSchema(schema: ZodTypeAny): object | null {
  if (schema instanceof ZodVoid) return null;
  const { $schema: _, ...rest } = zodToJsonSchema(schema, {
    $refStrategy: 'none',
    target: 'jsonSchema7',
    // Zod objects strip unknown keys; only .strict() ones (settings.set) reject them.
    removeAdditionalStrategy: 'strict',
  }) as Record<string, unknown>;
  return rest;
}

export function buildSchema(): object {
  const methods: Record<string, { params: object | null; result: object | null }> = {
    version: { params: toSchema(VersionHandshakeSchema), result: toSchema(VersionHandshakeSchema) },
  };
  for (const [name, def] of Object.entries(RpcMethods)) {
    methods[name] = { params: toSchema(def.request), result: toSchema(def.response) };
  }
  const notifications: Record<string, object | null> = {};
  for (const [name, schema] of Object.entries(RpcNotifications)) {
    notifications[name] = toSchema(schema);
  }
  return {
    $schema: 'http://json-schema.org/draft-07/schema#',
    title: 'Screentime daemon IPC contract',
    description:
      'JSON-RPC 2.0, one JSON message per line, over $XDG_RUNTIME_DIR/screentime/daemon.sock. ' +
      'Generated from packages/shared/src/ipc.ts by contract/harness/export-schema.ts; do not edit.',
    ipcVersion: IPC_VERSION,
    methods,
    notifications,
    clientNotifications: { 'browser.activeTab': toSchema(BrowserActiveTabSchema) },
  };
}

export function renderSchema(): string {
  return `${JSON.stringify(buildSchema(), null, 2)}\n`;
}

if (import.meta.main) {
  writeFileSync(SCHEMA_PATH, renderSchema());
  console.log(`wrote ${SCHEMA_PATH}`);
}
