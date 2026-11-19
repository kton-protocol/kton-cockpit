// Surfaces (decision of 18.11.: views in one window, a session in its own): the switch changes which lanes and areas the views show, in the same
// window; a surface's Claude session opens in its own window on the surface's folder.
//
// Read from a surfaces file in the workgroup format ({<surface>: {lanes: {<name>: {lane, dir}}, folder?}}),
// a setting. Areas come from the lanes query (lane → areas), never from the file.

import { readFile } from "node:fs/promises";
import type { Row } from "./queries.js";

export interface Surface {
  readonly name: string;
  /** Workstream paths of its lanes (the `lane` of each entry). */
  readonly lanes: readonly string[];
  readonly folder?: string;
}

export const ALL = "all";

export async function loadSurfaces(file: string | undefined): Promise<Surface[]> {
  if (!file) return [];
  try {
    const o = JSON.parse(await readFile(file, "utf8")) as Record<string, unknown>;
    return Object.entries(o)
      .filter(([k, v]) => !k.startsWith("_") && typeof v === "object" && v !== null)
      .map(([name, v]) => {
        const s = v as { lanes?: Record<string, { lane?: unknown }>; folder?: unknown };
        const lanes = Object.values(s.lanes ?? {})
          .map((l) => l.lane)
          .filter((x): x is string => typeof x === "string");
        return { name, lanes, ...(typeof s.folder === "string" ? { folder: s.folder } : {}) };
      });
  } catch {
    return [];
  }
}

/** lane → its areas, from the lanes query (areas is a comma-separated field). */
export function areasByLane(lanes: readonly Row[]): Map<string, string[]> {
  const m = new Map<string, string[]>();
  for (const r of lanes) {
    if (!r["lane"]) continue;
    m.set(r["lane"], (r["areas"] ?? "").split(",").map((a) => a.trim()).filter(Boolean));
  }
  return m;
}

/** The areas a surface shows; undefined = everything (the "all" view). */
export function surfaceAreas(surface: Surface | undefined, byLane: ReadonlyMap<string, readonly string[]>): Set<string> | undefined {
  if (!surface) return undefined;
  return new Set(surface.lanes.flatMap((l) => byLane.get(l) ?? []));
}

/** The folder a surface's session opens on: its own `folder`, else the pattern with ${surface}. */
export function surfaceFolder(surface: Surface, pattern: string | undefined): string | undefined {
  if (surface.folder) return surface.folder;
  if (pattern && pattern.includes("${surface}")) return pattern.split("${surface}").join(surface.name);
  return undefined;
}
