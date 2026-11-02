/**
 * Which media player the island shows. Pure JavaScript, no GNOME imports, so
 * Bun can test it (media.js does the D-Bus side).
 */

/**
 * One that is playing (the one that started most recently), else the one that
 * changed most recently. Players with nothing loaded are left out.
 * @param {{name: string, status: string, changedAt: number, title: string}[]} players
 */
export function pickPlayer(players) {
  const usable = players.filter((p) => p.title || p.status === 'Playing');
  if (usable.length === 0) return null;
  const playing = usable.filter((p) => p.status === 'Playing');
  const pool = playing.length ? playing : usable;
  return pool.reduce((best, p) => (p.changedAt > best.changedAt ? p : best));
}
