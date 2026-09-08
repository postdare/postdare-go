import { Navigate, createBrowserRouter, useLocation } from "react-router-dom";

import { AppShell } from "../layouts/AppShell";
import { useAuthStore } from "../store/auth";
import { ChangePasswordPage } from "../pages/ChangePasswordPage";
import { DashboardPage } from "../pages/DashboardPage";
import { DeployTaskDetailPage } from "../pages/DeployTaskDetailPage";
import { DeployTasksPage } from "../pages/DeployTasksPage";
import { LoginPage } from "../pages/LoginPage";
import { ProjectDetailPage } from "../pages/ProjectDetailPage";
import { ProjectFormPage } from "../pages/ProjectFormPage";
import { ProjectsPage } from "../pages/ProjectsPage";
import { ReportsPage } from "../pages/ReportsPage";
import { SettingsPage } from "../pages/SettingsPage";
import { WebhookEventsPage } from "../pages/WebhookEventsPage";
import { PublicReportPage } from "../pages/PublicReportPage";

function Protected() {
  const token = useAuthStore((state) => state.token);
  const user = useAuthStore((state) => state.user);
  const location = useLocation();
  if (!token) return <Navigate to="/login" replace />;
  if (user?.must_change_password && location.pathname !== "/change-password") {
    return <Navigate to="/change-password" replace />;
  }
  return <AppShell />;
}

export const router = createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  { path: "/reports/:reportId", element: <PublicReportPage /> },
  {
    path: "/",
    element: <Protected />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: "dashboard", element: <DashboardPage /> },
      { path: "change-password", element: <ChangePasswordPage /> },
      { path: "projects", element: <ProjectsPage /> },
      { path: "projects/new", element: <ProjectFormPage /> },
      { path: "projects/:id", element: <ProjectDetailPage /> },
      { path: "projects/:id/settings", element: <ProjectFormPage /> },
      { path: "deploy-tasks", element: <DeployTasksPage /> },
      { path: "deploy-tasks/:id", element: <DeployTaskDetailPage /> },
      { path: "reports", element: <ReportsPage /> },
      { path: "webhook-events", element: <WebhookEventsPage /> },
      { path: "settings", element: <SettingsPage /> }
    ]
  }
]);
