/** Runs external commands. Providers take one so tests can script the tools' output. */
export type Runner = {
  /** Runs a command to completion. */
  run(cmd: string[]): Promise<{ stdout: string; ok: boolean }>;
  /** Starts a long-running command, delivering each stdout line. */
  stream(
    cmd: string[],
    onLine: (line: string) => void,
  ): { stop: () => void; exited: Promise<void> };
  /** Whether a binary is on PATH. */
  has(binary: string): boolean;
};

export const bunRunner: Runner = {
  async run(cmd) {
    try {
      const proc = Bun.spawn(cmd, { stdout: 'pipe', stderr: 'ignore' });
      const stdout = await new Response(proc.stdout).text();
      return { stdout, ok: (await proc.exited) === 0 };
    } catch {
      return { stdout: '', ok: false };
    }
  },
  stream(cmd, onLine) {
    const proc = Bun.spawn(cmd, { stdout: 'pipe', stderr: 'ignore' });
    const exited = (async () => {
      const decoder = new TextDecoder();
      let buffer = '';
      for await (const chunk of proc.stdout) {
        buffer += decoder.decode(chunk, { stream: true });
        let i: number;
        // biome-ignore lint/suspicious/noAssignInExpressions: line splitter
        while ((i = buffer.indexOf('\n')) !== -1) {
          onLine(buffer.slice(0, i));
          buffer = buffer.slice(i + 1);
        }
      }
    })().catch(() => {});
    return { stop: () => proc.kill(), exited };
  },
  has: (binary) => Bun.which(binary) !== null,
};
