import type { RpcMethods } from '@screentime/shared';
import type { z } from 'zod';
import type { Bridge } from './bridge';

type Methods = typeof RpcMethods;
type Input<K extends keyof Methods> = z.input<Methods[K]['request']>;
type Output<K extends keyof Methods> = z.output<Methods[K]['response']>;

let bridge: Bridge | undefined;

export function setBridge(next: Bridge): void {
  bridge = next;
}

export function getBridge(): Bridge {
  if (!bridge) throw new Error('bridge not initialised');
  return bridge;
}

/**
 * Typed call to a daemon method. Types come from the same Zod schemas the
 * daemon validates with, so the UI and daemon can't drift apart silently.
 */
export function daemon<K extends keyof Methods>(
  method: K,
  // biome-ignore lint/suspicious/noConfusingVoidType: Zod's z.void() infers `void`, which is what no-argument methods use
  ...params: Input<K> extends void | undefined ? [params?: undefined] : [params: Input<K>]
): Promise<Output<K>> {
  return getBridge().call(method, params[0]) as Promise<Output<K>>;
}
