import { Activity, Boxes, CheckCircle2, XCircle } from "lucide-react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { dashboardSummary, recentDeployTasks } from "../api/postdareGo";
import { MetricCard } from "../components/MetricCard";
import { PageHeader } from "../components/PageHeader";
import { Badge, statusTone } from "../components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Table, Td, Th } from "../components/ui/table";
import { formatDate, shortCommit } from "../lib/utils";
import { useAuthStore } from "../store/auth";

export function DashboardPage() {
  const token = useAuthStore((state) => state.token);
  const summary = useQuery({ queryKey: ["dashboard-summary"], queryFn: () => dashboardSummary(token) });
  const recent = useQuery({ queryKey: ["recent-deploys"], queryFn: () => recentDeployTasks(token) });
  const data = summary.data?.data;

  return (
    <>
      <PageHeader title="Dashboard" />
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          label="Projects"
          value={
            <Link className="text-primary transition-colors hover:text-primary-strong hover:underline" to="/projects">
              {data?.project_total ?? "—"}
            </Link>
          }
          icon={<Boxes className="h-4 w-4" />}
        />
        <MetricCard label="Deploys today" value={data?.today_deploy_total ?? "—"} icon={<Activity className="h-4 w-4" />} />
        <MetricCard label="Succeeded" value={data?.today_success_total ?? "—"} icon={<CheckCircle2 className="h-4 w-4" />} />
        <MetricCard label="Failed" value={data?.today_failed_total ?? "—"} icon={<XCircle className="h-4 w-4" />} />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-[1fr_320px]">
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>Recent Deployments</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <div className="hidden overflow-x-auto md:block">
              <Table>
                <thead>
                  <tr>
                    <Th>Task</Th>
                    <Th>Project</Th>
                    <Th>Trigger</Th>
                    <Th>Commit</Th>
                    <Th>Status</Th>
                    <Th>Created</Th>
                  </tr>
                </thead>
                <tbody>
                  {(recent.data?.data ?? []).map((task) => (
                    <tr key={task.id} className="hover:bg-surface-2/70">
                      <Td>
                        <Link className="font-medium text-primary hover:underline" to={`/deploy-tasks/${task.id}`}>
                          #{task.id}
                        </Link>
                      </Td>
                      <Td>{task.project?.name ?? `Project ${task.project_id}`}</Td>
                      <Td>{task.trigger_type}</Td>
                      <Td className="font-mono text-xs">{shortCommit(task.commit_id)}</Td>
                      <Td>
                        <Badge tone={statusTone(task.status)}>{task.status}</Badge>
                      </Td>
                      <Td>{formatDate(task.created_at)}</Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
            <div className="divide-y divide-border md:hidden">
              {(recent.data?.data ?? []).map((task) => (
                <Link key={task.id} to={`/deploy-tasks/${task.id}`} className="block p-4 active:bg-surface-2/70">
                  <div className="flex items-center justify-between gap-3">
                    <span className="font-medium text-primary">#{task.id}</span>
                    <Badge tone={statusTone(task.status)}>{task.status}</Badge>
                  </div>
                  <div className="mt-1 truncate text-sm text-ink">{task.project?.name ?? `Project ${task.project_id}`}</div>
                  <div className="mt-1 text-xs text-muted">
                    {task.trigger_type} · <span className="font-mono">{shortCommit(task.commit_id)}</span> · {formatDate(task.created_at)}
                  </div>
                </Link>
              ))}
            </div>
          </CardContent>
        </Card>
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>Recent Failures</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            {(data?.recent_failed_tasks ?? []).length ? (
              data?.recent_failed_tasks.map((task) => (
                <Link key={task.id} to={`/deploy-tasks/${task.id}`} className="rounded-md border border-border p-3 transition-colors hover:bg-surface-2">
                  <div className="flex items-center justify-between gap-3">
                    <span className="font-medium">#{task.id}</span>
                    <Badge tone="failed">failed</Badge>
                  </div>
                  <div className="mt-2 text-sm text-muted">{task.fail_reason || task.current_stage || "failed"}</div>
                </Link>
              ))
            ) : (
              <div className="text-sm text-muted">No failed task</div>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  );
}
