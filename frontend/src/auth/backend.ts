import { backendUrl } from "@/src/middle/backend-url";

export type Owner = {
  id: number | string;
  username: string;
  display_name?: string;
  email?: string;
  role?: number;
};

export async function requestLogin(identifier: string, password: string) {
  const identity = identifier.includes("@")
    ? { email: identifier }
    : { username: identifier };

  return fetch(`${backendUrl()}/Authentication/login`, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ ...identity, password }),
    cache: "no-store",
  });
}

export async function exchangeLoginChallenge(code: string, id: string) {
  const query = new URLSearchParams({ code, id });
  const response = await fetch(
    `${backendUrl()}/Authentication/challenge?${query}`,
    {
      headers: { Accept: "application/json" },
      cache: "no-store",
    },
  );
  if (!response.ok) return null;

  const payload = (await response.json()) as { token?: unknown };
  return typeof payload.token === "string" && payload.token ? payload.token : null;
}

export async function fetchOwnerIdentity(token: string) {
  const response = await fetch(`${backendUrl()}/api/v1/me`, {
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
    },
    cache: "no-store",
  });
  if (!response.ok) return { status: response.status, owner: null };

  const owner = (await response.json()) as Partial<Owner>;
  if (
    (typeof owner.id !== "number" && typeof owner.id !== "string") ||
    typeof owner.username !== "string" ||
    !owner.username
  ) {
    return { status: 502, owner: null };
  }
  return { status: 200, owner: owner as Owner };
}

export async function revokeOwnerCredential(token: string) {
  await fetch(`${backendUrl()}/api/v1/logout`, {
    method: "POST",
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
    },
    cache: "no-store",
  });
}
