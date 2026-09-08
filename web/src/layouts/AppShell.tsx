import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { Activity, Boxes, GitBranch, LayoutDashboard, LogOut, Moon, Settings, Sun, Webhook } from "lucide-react";

import { Button } from "../components/ui/button";
import { cn } from "../lib/utils";
import { useAuthStore } from "../store/auth";

const navItems = [
  { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { to: "/projects", label: "Projects", icon: Boxes },
  { to: "/deploy-tasks", label: "Deployments", icon: Activity },
  { to: "/webhook-events", label: "Webhooks", icon: Webhook },
  { to: "/settings", label: "Settings", icon: Settings }
];

export function AppShell() {
  const navigate = useNavigate();
  const { user, logout, theme, toggleTheme } = useAuthStore();

  return (
    <div className="min-h-dvh bg-background text-ink">
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-64 border-r border-border bg-surface md:block">
        <div className="flex h-14 items-center gap-2 border-b border-border px-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary text-primary-ink">
            <GitBranch className="h-4 w-4" />
          </div>
          <div>
            <div className="text-sm font-semibold">Postdare Go</div>
            <div className="text-xs text-muted">Release console</div>
          </div>
        </div>
        <nav className="space-y-1 px-2 py-3">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  "flex h-9 items-center gap-2 rounded-md px-2.5 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-ink",
                  isActive && "bg-surface-2 text-ink"
                )
              }
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="md:pl-64">
        <header className="sticky top-0 z-10 flex h-14 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur md:px-6">
          <div className="flex items-center gap-2 md:hidden">
            <GitBranch className="h-4 w-4 text-primary" />
            <span className="font-semibold">Postdare Go</span>
          </div>
          <div className="hidden text-sm text-muted md:block">{user?.username ?? "admin"}</div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon" onClick={toggleTheme} aria-label="Toggle theme">
              {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                logout();
                navigate("/login");
              }}
              aria-label="Log out"
            >
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        </header>
        <main className="mx-auto w-full max-w-7xl overflow-x-clip px-4 pt-5 pb-[calc(4.5rem+env(safe-area-inset-bottom))] md:px-6 md:pb-5">
          <Outlet />
        </main>
        <nav className="fixed inset-x-0 bottom-0 z-20 grid grid-cols-5 border-t border-border bg-surface pb-[env(safe-area-inset-bottom)] md:hidden">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn("flex h-14 flex-col items-center justify-center gap-1 text-[11px] text-muted", isActive && "text-primary")
              }
            >
              <item.icon className="h-4 w-4" />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </div>
    </div>
  );
}
