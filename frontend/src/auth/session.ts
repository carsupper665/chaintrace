export const AUTH_COOKIE_NAME = "chaintrace_credential";

export function readCredential(request: Request) {
  const match = request.headers
    .get("cookie")
    ?.split(";")
    .map((part) => part.trim().split("="))
    .find(([name]) => name === AUTH_COOKIE_NAME);
  if (!match) return null;
  try {
    return decodeURIComponent(match.slice(1).join("="));
  } catch {
    return null;
  }
}

// Without this the cookie is a browser-session cookie that dies on browser close,
// which would cap the session well below the credential's own lifetime.
function credentialMaxAge(token: string) {
  const payload = token.split(".")[1];
  if (!payload) return null;
  try {
    const { exp } = JSON.parse(
      atob(payload.replace(/-/g, "+").replace(/_/g, "/")),
    );
    const seconds = Math.floor(exp - Date.now() / 1000);
    return seconds > 0 ? seconds : null;
  } catch {
    return null;
  }
}

export function credentialCookie(token: string) {
  const parts = [
    `${AUTH_COOKIE_NAME}=${encodeURIComponent(token)}`,
    "Path=/",
    "HttpOnly",
    "SameSite=Lax",
  ];
  const maxAge = credentialMaxAge(token);
  if (maxAge) parts.push(`Max-Age=${maxAge}`);
  if (process.env.NODE_ENV === "production") parts.push("Secure");
  return parts.join("; ");
}

export function clearedCredentialCookie() {
  return `${credentialCookie("")}; Max-Age=0`;
}
