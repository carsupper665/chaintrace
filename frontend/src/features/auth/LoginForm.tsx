"use client";

import { useState, type FormEvent } from "react";
import { loginOwner } from "@/src/auth/client";

export function LoginForm({ callbackFailed }: { callbackFailed: boolean }) {
  const [status, setStatus] = useState<"idle" | "submitting" | "waiting">(
    "idle",
  );
  const [error, setError] = useState(
    callbackFailed ? "驗證連結無效或已過期，請重新登入" : "",
  );

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setStatus("submitting");
    setError("");

    const result = await loginOwner({
      identifier: String(form.get("identifier") || "").trim(),
      password: String(form.get("password") || ""),
    });
    if (result.status === "waiting") {
      setStatus("waiting");
      return;
    }

    setStatus("idle");
    setError(result.message);
  }

  if (status === "waiting") {
    return (
      <div className="login-waiting" role="status">
        <span className="mail-mark">↗</span>
        <h2>檢查驗證郵件</h2>
        <p>驗證連結已寄出。請從同一台裝置開啟郵件，連結將在短時間後失效。</p>
        <button type="button" onClick={() => setStatus("idle")}>
          使用其他帳號
        </button>
      </div>
    );
  }

  return (
    <form className="login-form" onSubmit={submit}>
      <label>
        <span>Email 或使用者名稱</span>
        <input
          name="identifier"
          autoComplete="username"
          required
          autoFocus
        />
      </label>
      <label>
        <span>密碼</span>
        <input
          name="password"
          type="password"
          autoComplete="current-password"
          required
        />
      </label>
      {error && (
        <p className="login-error" role="alert">
          {error}
        </p>
      )}
      <button className="login-submit" type="submit" disabled={status === "submitting"}>
        {status === "submitting" ? "正在確認…" : "登入並寄送驗證郵件"}
      </button>
      <small>登入驗證不會在瀏覽器儲存原始憑證。</small>
    </form>
  );
}
