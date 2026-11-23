// One ask queue per window (a test window once ran ~130 cockpit asks at once and took its machine down). Every read the window makes of the cockpit (ask, list, templates, ui list/cat, show, doctor,
// version) goes through one gate: at most MAX_IN_FLIGHT at a time, first come first served. A cold index (at the start,
// and after a signature or anything else that changes the registry) is awaited once by a single ask: while cold, one at
// a time; the first ask that answers makes it warm, and the queued asks run then (never each its own cold build).
// Writes (say, publish, install, workflow run) are not queued behind reads (a person waits on them), and each marks
// the index cold. Only a read that started after the last write makes it warm: one that was running across a write
// answered from before it, and the index is not rebuilt for that write yet.
import type { Transport } from "./port.js";

export const MAX_IN_FLIGHT = 2;

export class AskGate {
  private inFlight = 0;
  private readonly waiting: Array<{ go: () => void; drop: (e: Error) => void; key?: string }> = [];
  private cold = true;
  /** counts markCold (each write): a read warms the index only when none came while it ran */
  private generation = 0;
  /** the most asks that ran at once (tests, the e2e's load check) */
  peak = 0;

  constructor(private readonly max = MAX_IN_FLIGHT) {}

  /** the index changed (a signature, a federation pull): the next ask runs alone */
  markCold(): void {
    this.generation++;
    this.cold = true;
  }
  get isCold(): boolean {
    return this.cold;
  }
  get queued(): number {
    return this.waiting.length;
  }

  /**
   * Run `fn` when a slot is free. With `supersede` (e.g. a view block's key), an ask of the same key still waiting is
   * dropped (rejected with Superseded): a reader moving fast through items asks once for where it stopped, not for
   * every item it passed.
   */
  async run<T>(fn: () => Promise<T>, supersede?: string): Promise<T> {
    if (supersede !== undefined) {
      for (let i = this.waiting.length - 1; i >= 0; i--) {
        const w = this.waiting[i];
        if (w?.key === supersede) {
          this.waiting.splice(i, 1);
          w.drop(new Superseded(supersede));
        }
      }
    }
    // the slot is taken synchronously (now, or by release() for a waiting ask): never more than the limit at once
    if (this.inFlight < this.limit() && this.waiting.length === 0) this.inFlight++;
    else await new Promise<void>((go, drop) => this.waiting.push({ go, drop, ...(supersede !== undefined ? { key: supersede } : {}) }));
    this.peak = Math.max(this.peak, this.inFlight);
    const started = this.generation;
    let ok = false;
    try {
      const r = await fn();
      ok = true;
      return r;
    } finally {
      this.inFlight--;
      if (ok && started === this.generation) this.cold = false;
      this.release();
    }
  }

  private limit(): number {
    return this.cold ? 1 : this.max;
  }
  private release(): void {
    while (this.waiting.length && this.inFlight < this.limit()) {
      this.inFlight++;
      (this.waiting.shift() as { go: () => void }).go();
    }
  }
}

/** An ask dropped while it waited, because a newer one of the same key came. */
export class Superseded extends Error {
  constructor(key: string) {
    super(`superseded by a newer ask of ${key}`);
    this.name = "Superseded";
  }
}

/** The Transport methods that go through the queue; every method of Transport is in READS or WRITES (test/gate.test.ts). */
export const READS = ["ask", "templates", "list", "doctor", "version", "uiList", "uiCat", "graph"] as const;
/** The Transport methods that pass the queue and mark the index cold. */
export const WRITES = ["say", "publish", "install", "workflowRun"] as const;

/** The transport with its reads through the gate; its writes pass and mark the index cold. */
export function gated(t: Transport, gate: AskGate): Transport {
  const out: Record<string, unknown> = {};
  for (const m of READS) out[m] = (...a: unknown[]) => gate.run(() => (t[m] as (...x: unknown[]) => Promise<unknown>).apply(t, a), m === "ask" ? (a[1] as { supersede?: string } | undefined)?.supersede : undefined);
  for (const m of WRITES)
    out[m] = async (...a: unknown[]) => {
      try {
        return await (t[m] as (...x: unknown[]) => Promise<unknown>).apply(t, a);
      } finally {
        gate.markCold();
      }
    };
  return out as unknown as Transport;
}
