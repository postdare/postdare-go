import { FormEvent, useState } from "react";
import { GitBranch } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { login } from "../api/postdareGo";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { useAuthStore } from "../store/auth";

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const setSession = useAuthStore((state) => state.setSession);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      const res = await login(username, password);
      setSession(res.data.token, res.data.user);
      const next = searchParams.get("next");
      navigate(res.data.user.must_change_password ? "/change-password" : next || "/dashboard", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background px-4 py-10 text-ink">
      <div className="w-full max-w-sm">
        <div className="mb-5 flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-ink">
            <GitBranch className="h-4 w-4" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">Postdare Go</h1>
            <p className="text-sm text-muted">Release console</p>
          </div>
        </div>
        <Card>
          <CardContent className="p-4">
            <form className="grid gap-3" onSubmit={onSubmit}>
              <label className="grid gap-1.5">
                <span className="text-xs font-medium text-muted">Username</span>
                <Input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
              </label>
              <label className="grid gap-1.5">
                <span className="text-xs font-medium text-muted">Password</span>
                <Input value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="current-password" />
              </label>
              {error ? <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div> : null}
              <Button variant="primary" disabled={loading}>
                {loading ? "Signing in" : "Sign in"}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
