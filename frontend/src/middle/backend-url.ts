// The Go API listens on 7794 by default while the frontend runs on 3000, so a
// local checkout that has not copied .env.example still has to reach the right
// process. Production must be explicit: a wrong origin there is a silent
// outage, not a convenience.
const DEVELOPMENT_BACKEND_URL = "http://localhost:7794";

/**
 * Origin of the Go API. Server-side only — the browser talks to the same-origin
 * BFF routes under `app/api`, never to this URL.
 */
export function backendUrl() {
  const configured = process.env.CHAINTRACE_BACKEND_URL?.trim().replace(/\/+$/, "");
  if (configured) return configured;
  if (process.env.NODE_ENV === "production") {
    throw new Error("CHAINTRACE_BACKEND_URL is not configured");
  }
  return DEVELOPMENT_BACKEND_URL;
}
