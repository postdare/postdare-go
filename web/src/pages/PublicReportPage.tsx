import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, FileSearch, GitCommitHorizontal } from "lucide-react";
import { useParams } from "react-router-dom";

import { getPublicReport } from "../api/postdareGo";
import { Badge } from "../components/ui/badge";
import { ReportBody, reportTypeLabel } from "../components/ReportView";

function consumeReportToken(reportId: string) {
  const storageKey = `postdare.report.${reportId}`;
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const fragmentToken = fragment.get("token")?.trim();
  if (fragmentToken) {
    try { sessionStorage.setItem(storageKey, fragmentToken); } catch { /* private storage can be unavailable */ }
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
    return fragmentToken;
  }
  try { return sessionStorage.getItem(storageKey) ?? ""; } catch { return ""; }
}

export function PublicReportPage() {
  const { reportId = "" } = useParams();
  const [token] = useState(() => consumeReportToken(reportId));
  const report = useQuery({
    queryKey: ["public-report", reportId, token],
    queryFn: () => getPublicReport(reportId, token),
    enabled: Boolean(reportId && token),
    retry: false
  });
  const data = report.data?.data;

  if (!token) {
    return <ReportMessage title="Report link is incomplete" body="Open the complete share link from your deployment notification." />;
  }
  if (report.isLoading) {
    return <ReportSkeleton />;
  }
  if (report.isError || !data) {
    return <ReportMessage title="Report is unavailable" body="This link may have been revoked or replaced. Request a new share link from Postdare." />;
  }

  return (
    <main className="report-shell">
      <header className="report-header">
        <div className="report-header-inner">
          <div className="flex min-w-0 items-start gap-3">
            <div className="report-mark" aria-hidden="true"><FileSearch className="h-5 w-5" /></div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-xl font-semibold tracking-[-0.02em]">{data.project_name || reportTypeLabel(data.type)}</h1>
                <Badge tone={data.status === "success" ? "success" : data.status === "failed" ? "failed" : "canceled"}>{data.status}</Badge>
              </div>
              <p className="mt-1 text-sm text-muted">{reportTypeLabel(data.type)}</p>
            </div>
          </div>
          <div className="report-commit">
            <GitCommitHorizontal className="h-4 w-4" />
            <span>{shortCommit(data.before_commit_id)} → {shortCommit(data.commit_id)}</span>
          </div>
        </div>
      </header>

      <div className="report-content">
        <ReportBody report={data} />
      </div>
    </main>
  );
}

function shortCommit(value: string) { return value && value.length > 10 ? value.slice(0, 10) : value || "unknown"; }

function ReportMessage({ title, body }: { title: string; body: string }) {
  return <main className="grid min-h-dvh place-items-center p-5"><div className="w-full max-w-md border-y border-border py-8 text-center"><AlertTriangle className="mx-auto h-6 w-6 text-warning" /><h1 className="mt-3 text-lg font-semibold">{title}</h1><p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-muted">{body}</p></div></main>;
}

function ReportSkeleton() {
  return <main className="report-shell animate-pulse"><div className="h-20 border-b border-border bg-surface" /><div className="report-content"><div className="h-40 rounded-md bg-surface" /><div className="mt-8 h-72 rounded-md bg-surface" /></div></main>;
}
