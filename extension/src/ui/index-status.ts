// What the window is waiting for, in the status bar (a user: "I cannot see that I am waiting for my kton to load, and
// the first time can be quite slow"). Pure: the gate's state (transport/gate.ts) and the time give the text.
import type { GateState } from "../transport/gate.js";

/** how long "kton: ready" stays after a build */
export const READY_MS = 5000;

const two = (n: number) => String(n).padStart(2, "0");
export function hhmm(ms: number): string {
  const d = new Date(ms);
  return `${two(d.getHours())}:${two(d.getMinutes())}`;
}
export function seconds(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 120 ? `${s} s` : `${Math.floor(s / 60)} min ${s % 60} s`;
}

/** The status bar item: building (spinner, since, how long), ready (briefly), n waiting, or the last failure; else none. */
export function indexStatusText(s: GateState, now: number): { text: string; tooltip: string } | undefined {
  if (s.buildingSince !== undefined)
    return {
      text: `$(sync~spin) kton: building the index… since ${hhmm(s.buildingSince)} (${seconds(now - s.buildingSince)})${s.queued ? ` · ${s.queued} waiting` : ""}`,
      tooltip: "The first ask after a change (a signature, a new source, the window's start) builds the cockpit's index; the other asks wait for it. The first load can take 1–2 min.",
    };
  if (s.lastError?.unreachable && s.cold)
    return {
      text: `$(error) kton: index not reachable · ${s.lastError.message.slice(0, 60)}`,
      tooltip: `The index did not answer at ${hhmm(s.lastError.at)}: ${s.lastError.message}. Run \`cockpit doctor\` to check the cockpit and its index daemon. The next ask (a poll, or Retry in a view) tries again.`,
    };
  if (s.lastError && s.cold)
    return { text: `$(warning) kton: not loaded · ${s.lastError.message.slice(0, 60)}`, tooltip: `The last ask failed at ${hhmm(s.lastError.at)}: ${s.lastError.message}. The next ask (a poll, or Retry in a view) tries again.` };
  if (s.queued > 0) return { text: `$(clock) kton: ${s.queued} waiting`, tooltip: `${s.inFlight} asks running, ${s.queued} waiting (at most 2 at a time).` };
  if (s.readyAt !== undefined && now - s.readyAt < READY_MS) return { text: "$(check) kton: ready", tooltip: `The index was built at ${hhmm(s.readyAt)}.` };
  return undefined;
}
