import { LoginForm } from "@/src/features/auth/LoginForm";

type LoginPageProps = {
  searchParams: Promise<{ error?: string }>;
};

export default async function LoginPage({ searchParams }: LoginPageProps) {
  const { error } = await searchParams;

  return (
    <main className="login-shell">
      <section className="login-brand" aria-labelledby="login-heading">
        <span className="login-kicker">CHAINTRACE / OWNER ACCESS</span>
        <h1 id="login-heading">回到調查現場</h1>
        <p>
          以既有 Owner 帳號登入。我們會寄出一次性驗證連結，完成確認後才會開啟調查工作區。
        </p>
        <div className="login-signal" aria-hidden="true">
          <span />
          <span />
          <span />
          <span />
        </div>
      </section>

      <section className="login-panel" aria-label="Owner 登入">
        <div className="login-panel-heading">
          <span>SECURE ENTRY</span>
          <strong>01</strong>
        </div>
        <LoginForm callbackFailed={error === "callback"} />
      </section>
    </main>
  );
}
