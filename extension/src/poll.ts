// One polling scheduler (contract C1.6). Polling is done by code, never by an LLM; the interval is
// settable (test hooks); jobs never overlap themselves; the status bar shows what runs. Regular and
// long work moves to cockpit-runtime's jobs module (ADR-017); events replace polling with v0.6.0.

export interface Clock {
  now(): number;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
}

export const realClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

export interface JobStatus {
  readonly name: string;
  readonly intervalMs: number;
  readonly running: boolean;
  readonly lastRun?: number;
  readonly lastError?: string;
  readonly runs: number;
  /** the delay to the next run (the interval, or backed off) */
  readonly delayMs: number;
}

interface Job {
  name: string;
  intervalMs: number;
  fn: () => Promise<void>;
  running: boolean;
  lastRun?: number;
  lastError?: string;
  runs: number;
  handle?: unknown;
  /** Bumped by setInterval: a tick that started under an older value does not schedule again. */
  gen: number;
  /** the delay to the next tick: the interval, doubled (up to MAX_BACKOFF_MS) while a tick takes longer than it */
  delayMs: number;
  /** stopped while the window is away (past pauseAfterMs); the focus starts it again */
  paused?: boolean;
}

/** A poll whose last run took longer than its interval waits twice as long next time, up to this (5 min). */
export const MAX_BACKOFF_MS = 300_000;
/** The next delay of a poll: its interval when the last run was quicker than that, else twice the last delay (capped). */
export function nextDelay(intervalMs: number, lastDelayMs: number, tookMs: number): number {
  return tookMs > intervalMs ? Math.min(Math.max(lastDelayMs, intervalMs) * 2, Math.max(MAX_BACKOFF_MS, intervalMs)) : intervalMs;
}

/** The idle backoff: how long the window may be unfocused before its polls slow down, and before they stop. */
export interface IdlePolicy {
  /** unfocused this long: polls run at most every slowMs */
  readonly idleAfterMs: number;
  /** unfocused this long: polls stop until the window has the focus again */
  readonly pauseAfterMs: number;
  /** the interval while idle (5 minutes) */
  readonly slowMs: number;
}
export const DEFAULT_IDLE: IdlePolicy = { idleAfterMs: 5 * 60_000, pauseAfterMs: 30 * 60_000, slowMs: 5 * 60_000 };
/** The policy from the settings (minutes); a pause before the slow-down is taken as both at the pause. */
export function idlePolicy(idleAfterMinutes: number, pauseAfterMinutes: number): IdlePolicy {
  const idle = Math.max(1, idleAfterMinutes) * 60_000;
  const pause = Math.max(1, pauseAfterMinutes) * 60_000;
  return { idleAfterMs: Math.min(idle, pause), pauseAfterMs: pause, slowMs: DEFAULT_IDLE.slowMs };
}
export type PollState = { readonly state: "active" } | { readonly state: "slow" | "paused"; readonly since: number };

export class Scheduler {
  private readonly jobs = new Map<string, Job>();
  private stopped = false;
  /** when the window lost the focus (undefined: it has it) */
  private unfocusedSince: number | undefined;
  private shown: PollState["state"] = "active";
  /** told when the polls slow down, stop or run normally again (the status bar says it) */
  onState?: (s: PollState) => void;

  constructor(
    private readonly clock: Clock = realClock,
    private readonly idle: IdlePolicy = DEFAULT_IDLE,
  ) {}

  /** The poll state now: active, slow (unfocused past idleAfterMs) or paused (past pauseAfterMs). */
  pollState(): PollState {
    if (this.unfocusedSince === undefined) return { state: "active" };
    const away = this.clock.now() - this.unfocusedSince;
    if (away >= this.idle.pauseAfterMs) return { state: "paused", since: this.unfocusedSince };
    if (away >= this.idle.idleAfterMs) return { state: "slow", since: this.unfocusedSince };
    return { state: "active" };
  }

  private tell(): PollState {
    const s = this.pollState();
    if (s.state !== this.shown) {
      this.shown = s.state;
      this.onState?.(s);
    }
    return s;
  }

  /**
   * The window's focus (VS Code's window state). Losing it starts the idle clock; getting it back runs every poll once
   * now and then at its own interval again. Writes and the user's own asks never wait for this (runNow is not held).
   */
  setFocused(focused: boolean): void {
    if (!focused) {
      if (this.unfocusedSince === undefined) this.unfocusedSince = this.clock.now();
      // a check when the window would slow down and when it would stop, so the status bar says it on time
      for (const ms of [this.idle.idleAfterMs, this.idle.pauseAfterMs]) this.clock.setTimeout(() => void this.tell(), ms + 1);
      return;
    }
    const was = this.unfocusedSince;
    this.unfocusedSince = undefined;
    this.tell();
    if (was === undefined) return;
    for (const job of this.jobs.values()) {
      if (job.paused || this.clock.now() - was >= this.idle.idleAfterMs) {
        job.paused = false;
        job.gen++;
        if (job.handle !== undefined) this.clock.clearTimeout(job.handle);
        job.delayMs = job.intervalMs;
        const gen = job.gen;
        void this.tick(job).then(() => {
          if (job.gen === gen) this.schedule(job, job.delayMs);
        });
      }
    }
  }

  /** Register a poll. A second job under the same name is refused: one poll per thing. */
  add(name: string, intervalMs: number, fn: () => Promise<void>): void {
    if (this.jobs.has(name)) throw new Error(`a poll named ${name} exists`);
    if (!(intervalMs >= 1000)) throw new Error("a poll runs at most once a second");
    const job: Job = { name, intervalMs, fn, running: false, runs: 0, gen: 0, delayMs: intervalMs };
    this.jobs.set(name, job);
    this.schedule(job, 0);
  }

  setInterval(name: string, intervalMs: number): void {
    const job = this.jobs.get(name);
    if (!job) throw new Error(`no poll named ${name}`);
    job.intervalMs = intervalMs;
    job.delayMs = intervalMs;
    job.gen++; // a tick running now must not start a second timer chain
    if (job.handle !== undefined) this.clock.clearTimeout(job.handle);
    this.schedule(job, intervalMs);
  }

  /** Run one poll now (the user's Refresh). Skipped while it is already running. */
  async runNow(name: string): Promise<void> {
    const job = this.jobs.get(name);
    if (job) await this.tick(job);
  }

  status(): JobStatus[] {
    return [...this.jobs.values()].map((j) => ({
      name: j.name,
      intervalMs: j.intervalMs,
      running: j.running,
      runs: j.runs,
      delayMs: j.delayMs,
      ...(j.lastRun !== undefined ? { lastRun: j.lastRun } : {}),
      ...(j.lastError !== undefined ? { lastError: j.lastError } : {}),
    }));
  }

  stop(): void {
    this.stopped = true;
    for (const j of this.jobs.values()) if (j.handle !== undefined) this.clock.clearTimeout(j.handle);
  }

  private schedule(job: Job, delay: number): void {
    if (this.stopped) return;
    const gen = job.gen;
    job.handle = this.clock.setTimeout(() => {
      const st = this.tell();
      // away past pauseAfterMs: no poll until the focus comes back (setFocused runs it then)
      if (st.state === "paused") {
        job.paused = true;
        return;
      }
      void this.tick(job).then(() => {
        if (job.gen !== gen) return;
        // away past idleAfterMs: at most every slowMs
        this.schedule(job, this.pollState().state === "slow" ? Math.max(job.delayMs, this.idle.slowMs) : job.delayMs);
      });
    }, delay);
  }

  private async tick(job: Job): Promise<void> {
    if (job.running || this.stopped) return;
    job.running = true;
    const t0 = this.clock.now();
    try {
      await job.fn();
      delete job.lastError;
    } catch (e) {
      job.lastError = (e as Error).message;
    } finally {
      job.running = false;
      job.runs++;
      job.lastRun = this.clock.now();
      job.delayMs = nextDelay(job.intervalMs, job.delayMs, job.lastRun - t0);
    }
  }
}
