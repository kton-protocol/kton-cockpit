// One ask queue per window (a test window once ran ~130 cockpit asks at once and took its machine down). Every read the window makes of the cockpit (ask, list, templates, ui list/cat, show, doctor,
// version) goes through one gate: at most MAX_IN_FLIGHT at a time, first come first served. A cold index (at the start,
// and after a signature or anything else that changes the registry) is awaited once by a single ask: while cold, one at
// a time; the first ask that answers makes it warm, and the queued asks run then (never each its own cold build).
// Writes (say, publish, install, workflow run) are not queued behind reads (a person waits on them), and each marks
// the index cold.
import type { Transport } from "./port.js";

export const MAX_IN_FLIGHT = 2;

export class AskGate {
  private inFlight = 0;
  private readonly waiting: Array<() => void> = [];
  private cold = true;
  /** the most asks that ran at once (tests, the e2e's load check) */
  peak = 0;

  constructor(private readonly max = MAX_IN_FLIGHT) {}

  /** the index changed (a signature, a federation pull): the next ask runs alone */
  markCold(): void {
    this.cold = true;
  }
  get isCold(): boolean {
    return this.cold;
  }
  get queued(): number {
    return this.waiting.length;
  }

  async run<T>(fn: () => Promise<T>): Promise<T> {
    // the slot is taken synchronously (now, or by release() for a waiting ask): never more than the limit at once
    if (this.inFlight < this.limit() && this.waiting.length === 0) this.inFlight++;
    else await new Promise<void>((resolve) => this.waiting.push(resolve));
    this.peak = Math.max(this.peak, this.inFlight);
    let ok = false;
    try {
      const r = await fn();
      ok = true;
      return r;
    } finally {
      this.inFlight--;
      if (ok) this.cold = false;
      this.release();
    }
  }

  private limit(): number {
    return this.cold ? 1 : this.max;
  }
  private release(): void {
    while (this.waiting.length && this.inFlight < this.limit()) {
      this.inFlight++;
      (this.waiting.shift() as () => void)();
    }
  }
}

const READS = ["ask", "templates", "list", "doctor", "version", "uiList", "uiCat", "graph"] as const;
const WRITES = ["say", "publish", "install", "workflowRun"] as const;

/** The transport with its reads through the gate; its writes pass and mark the index cold. */
export function gated(t: Transport, gate: AskGate): Transport {
  const out: Record<string, unknown> = {};
  for (const m of READS) out[m] = (...a: unknown[]) => gate.run(() => (t[m] as (...x: unknown[]) => Promise<unknown>).apply(t, a));
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
