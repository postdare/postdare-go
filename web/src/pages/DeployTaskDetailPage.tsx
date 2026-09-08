import { AlertTriangle, Ban, FileSearch, RotateCcw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest, streamURL } from "../api/client";
import { getDeployLog, getDeployTask, listDeployTaskReports } from "../api/postdareGo";
import type { DataResponse, DeployTask } from "../api/types";
import { LogViewer } from "../components/LogViewer";
import { PageHeader } from "../components/PageHeader";
import { ReportBody, reportTypeLabel } from "../components/ReportView";
import { Badge, statusTone } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { useEventStream } from "../hooks/useEventStream";
import { cn, formatDate, shortCommit } from "../lib/utils";
import { useAuthStore } from "../store/auth";

export function DeployTaskDetailPage() {
  const { id } = useParams();
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const task = useQuery({ queryKey: ["deploy-task", id], queryFn: () => getDeployTask(id!, token), enabled: Boolean(id), refetchInterval: 4000 });
  const log = useQuery({ queryKey: ["deploy-log", id], queryFn: () => getDeployLog(id!, token), enabled: Boolean(id) });
  // Reports are captured by deploy stages, so they appear mid-run; polling while
  // the task is live avoids a stale "no report" until the next manual refresh.
  const reports = useQuery({ queryKey: ["deploy-task-reports", id], queryFn: () => listDeployTaskReports(id!, token), enabled: Boolean(id), refetchInterval: 4000 });
  const data = task.data?.data;
  const isLive = data?.status === "pending" || data?.status === "running";
  const liveLines = useEventStream(id && token && isLive ? streamURL(`/api/v1/deploy-tasks/${id}/logs/stream`, token) : undefined);
  const cancel = useMutation({
    mutationFn: () => apiRequest<DataResponse<DeployTask>>(`/api/v1/deploy-tasks/${id}/cancel`, { method: "POST", body: JSON.stringify({}) }, token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["deploy-task", id] })
  });

  return (
    <>
      <PageHeader
        title={data ? `Deployment #${data.id}` : "Deployment"}
        description={data ? `${data.trigger_type} · ${data.git_provider ?? "git"} · ${data.branch ?? "branch"} · ${shortCommit(data.commit_id)}` : "Loading deployment"}
        actions={
          <Button variant="secondary" onClick={() => cancel.mutate()} disabled={!data || !["pending", "running"].includes(data.status)}>
            <Ban className="h-4 w-4" />
            Cancel
          </Button>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[360px_1fr]">
        <div className="grid gap-4">
          <Card>
            <CardHeader>
              <CardTitle>Status</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm text-muted">Task status</span>
                <Badge className="shrink-0" tone={statusTone(data?.status)}>{data?.status ?? "—"}</Badge>
              </div>
              <Info label="Project" value={data?.project ? <Link className="text-primary hover:underline" to={`/projects/${data.project.id}`}>{data.project.name}</Link> : `Project ${data?.project_id ?? "—"}`} />
              <Info label="Current stage" value={data?.current_stage || "—"} />
              <Info label="Commit" value={data?.commit_id ? `${shortCommit(data.commit_id)} · ${data.commit_message ?? ""}` : "—"} />
              <Info label="Author" value={data?.commit_author || "—"} />
              <Info label="Started" value={formatDate(data?.started_at)} />
              <Info label="Finished" value={formatDate(data?.finished_at)} />
            </CardContent>
          </Card>
          {data?.fail_reason ? (
            <Card className="border-danger/35">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-danger">
                  <AlertTriangle className="h-4 w-4" />
                  Failure
                </CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-danger">{data.fail_reason}</CardContent>
            </Card>
          ) : null}
          <Card>
            <CardHeader>
              <CardTitle>Stages</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-2">
              {(data?.stages ?? []).length ? (
                (data?.stages ?? []).map((stage) => (
                  <div key={stage.id} className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{stage.name}</div>
                      {stage.error_message ? <div className="mt-1 break-words break-all text-xs text-danger">{stage.error_message}</div> : null}
                    </div>
                    <Badge className="shrink-0" tone={statusTone(stage.status)}>{stage.status}</Badge>
                  </div>
                ))
              ) : (
                <div className="text-sm text-muted">No stage data.</div>
              )}
            </CardContent>
          </Card>
        </div>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Deploy Log</CardTitle>
            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => log.refetch()} disabled={log.isFetching}>
              <RotateCcw className={cn("h-4 w-4 text-muted", log.isFetching && "animate-spin")} />
            </Button>
          </CardHeader>
          <CardContent>
            <LogViewer log={log.data?.data.log} liveLines={liveLines} />
          </CardContent>
        </Card>
      </div>
      {(reports.data?.data ?? []).map((report) => (
        <Card key={report.id} className="mt-4">
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <FileSearch className="h-4 w-4" />
              {reportTypeLabel(report.type)}
            </CardTitle>
            <Badge tone={statusTone(report.status)}>{report.status}</Badge>
          </CardHeader>
          <CardContent>
            <ReportBody report={report} />
          </CardContent>
        </Card>
      ))}
    </>
  );
}

function Info({ label, value }: { label: string; value?: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-0.5 break-words break-all text-sm text-ink">{value || "—"}</div>
    </div>
  );
}
