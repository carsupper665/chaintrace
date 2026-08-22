export type LoginCredentials = {
  identifier: string;
  password: string;
};

export type LoginResult =
  | { status: "waiting" }
  | { status: "error"; message: string };

type Requester = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;

export async function loginOwner(
  credentials: LoginCredentials,
  request: Requester = fetch,
): Promise<LoginResult> {
  try {
    const response = await request("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(credentials),
    });
    if (response.status === 202) return { status: "waiting" };

    const payload = (await response.json()) as { error?: string };
    return {
      status: "error",
      message: payload.error || "登入失敗，請稍後再試",
    };
  } catch {
    return { status: "error", message: "登入失敗，請稍後再試" };
  }
}
