// Which origins a frame accepts kton: messages from (H9, VERTICLE-HOSTS §13): its own location.origin,
// vscode-webview://, and named hosts from a host-configured list. Never "null" (a sandboxed or opaque
// origin), no wildcards (an entry with "*" is ignored, never matched).
export function originAllowed(origin, own, configured = []) {
  if (typeof origin !== "string" || origin === "" || origin === "null") return false;
  if (origin === own) return true;
  if (origin.startsWith("vscode-webview://")) return true;
  return configured.some((o) => typeof o === "string" && o !== "null" && !o.includes("*") && o === origin);
}
