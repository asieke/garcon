import type { RequestRow } from './model';

export function hasTokens(row: RequestRow): boolean {
  return (row.input || 0) + (row.cache_read || 0) + (row.cache_write || 0) + (row.output || 0) > 0;
}

/** Baseline history, then consume each request once, even across streaming updates. */
export class LiveRequests {
  private latest: number | null = null;
  private waiting = new Map<number, { row: RequestRow; arrived: number }>();
  private pending = new Map<number, { row: RequestRow; arrived: number }>();

  update(rows: RequestRow[], hideZeroTokens: boolean, now: number): boolean {
    const newest = Math.max(0, ...rows.map(row => row.sequence));
    const reset = this.latest !== null && newest < this.latest;
    if (this.latest === null || reset) {
      this.latest = newest;
      this.waiting.clear();
      this.pending.clear();
      return reset;
    }
    for (const row of [...rows].sort((a, b) => a.sequence - b.sequence)) {
      const pending = this.pending.get(row.sequence);
      if (pending) pending.row = row;
      if (row.sequence > this.latest || this.waiting.has(row.sequence)) {
        const entry = this.waiting.get(row.sequence) ?? { row, arrived: now };
        entry.row = row;
        if (!hideZeroTokens || hasTokens(row)) {
          this.pending.set(row.sequence, { row, arrived: now });
          this.waiting.delete(row.sequence);
        } else if (row.state === 'streaming') {
          this.waiting.set(row.sequence, entry);
        } else {
          this.waiting.delete(row.sequence);
        }
      }
    }
    this.latest = newest;
    // A burst or a paused tab should not leave minutes of stale activity to play.
    for (const entries of [this.pending, this.waiting]) {
      for (const [sequence, entry] of entries) {
        if (entries === this.pending
          ? now - entry.arrived > 30_000 || (hideZeroTokens && !hasTokens(entry.row))
          : !rows.some(row => row.sequence === sequence)) entries.delete(sequence);
      }
      while (entries.size > 30) entries.delete(entries.keys().next().value!);
    }
    return reset;
  }

  next(now: number): RequestRow | undefined {
    for (const [sequence, entry] of this.pending) {
      this.pending.delete(sequence);
      if (now - entry.arrived <= 30_000) return entry.row;
    }
  }
}
