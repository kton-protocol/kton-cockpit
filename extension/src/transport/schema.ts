// Shape checks for every JSON answer the shell parses (review check 5: the shell side of the
// schema tests; the cockpit side tests its own output). A wrong shape is a TransportError, never a
// silently half-read value.

import type { AskResult, DoctorReport, PublishResult, SayResult } from "./port.js";
import { TransportError } from "./port.js";

function obj(v: unknown, what: string): Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) throw new TransportError(`${what}: expected an object`, 0, "");
  return v as Record<string, unknown>;
}

function str(o: Record<string, unknown>, k: string, what: string, optional = false): void {
  const v = o[k];
  if (v === undefined && optional) return;
  if (typeof v !== "string") throw new TransportError(`${what}: field ${k} must be a string`, 0, "");
}

export function assertSayResult(v: unknown): SayResult {
  const o = obj(v, "say");
  str(o, "claimId", "say");
  if (!/^sha256:[0-9a-f]{64}$/.test(o["claimId"] as string)) throw new TransportError("say: claimId is not a sha256 id", 0, "");
  str(o, "confirmation", "say", true);
  str(o, "level", "say", true);
  return { confirmation: "", ...o } as unknown as SayResult;
}

export function assertAskResult(v: unknown): AskResult {
  const o = obj(v, "ask");
  str(o, "query", "ask");
  str(o, "ref", "ask", true);
  str(o, "raw", "ask", true);
  if (o["results"] !== undefined) {
    const r = obj(o["results"], "ask results");
    const head = obj(r["head"], "ask results head");
    if (!Array.isArray(head["vars"])) throw new TransportError("ask results: head.vars must be a list", 0, "");
    const res = obj(r["results"], "ask results.results");
    if (!Array.isArray(res["bindings"])) throw new TransportError("ask results: bindings must be a list", 0, "");
  }
  return { ref: "", raw: "", ...o } as unknown as AskResult;
}

export function assertPublishResult(v: unknown): PublishResult {
  const o = obj(v, "publish");
  str(o, "revision", "publish", true);
  str(o, "fotonId", "publish", true);
  return { revision: "", ...o } as unknown as PublishResult;
}

export function assertDoctorReport(v: unknown): DoctorReport {
  const o = obj(v, "doctor");
  if (typeof o["ready"] !== "boolean") throw new TransportError("doctor: ready must be a boolean", 0, "");
  if (!Array.isArray(o["steps"])) throw new TransportError("doctor: steps must be a list", 0, "");
  obj(o["fields"], "doctor fields");
  return o as unknown as DoctorReport;
}

/**
 * workflow run and install: the shell reads no field of these answers (it shows them as they are), so
 * the check is that the answer is one JSON object; review-checks.test.ts holds that nothing reads more.
 */
export function assertObject(v: unknown, what: string): Readonly<Record<string, unknown>> {
  return obj(v, what);
}
