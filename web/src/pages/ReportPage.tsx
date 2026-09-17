import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, FileSearch, GitCommitHorizontal } from "lucide-react";
import { useParams } from "react-router-dom";

import { getPublicReport, getReport } from "../api/postdareGo";
import { Badge } from "../components/ui/badge";
import { Toaster } from "../components/ui/toast";
import { ReportBody, reportTypeLabel } from "../components/ReportView";
import { shortCommit } from "../lib/utils";
import { useAuthStore } from "../store/auth";

/** The credential a visit presents for a report: the token in a share link, or
 *  the session of someone already signed in to the console. */
type ReportAccess = { kind: "share"; token: string } | { kind: "session" };

/** What the address bar offers for this report, and which credential wins when
 *  more than one is available.
 *
 *  A token in the link is honoured even when a session exists: opening a share
 *  link is how the link itself gets checked, and answering it from the session
 *  would keep a revoked link looking alive. A token remembered from an earlier
 *  visit in this tab is the weakest claim of the three and is only consulted
 *  when nothing better was presented -- the session is a live credential, the
 *  remembered one may have been revoked since.
 *
 *  Pure: this runs on every render, so it must not consume what it reads. The
 *  link is spent separately, in an effect. */
function readReportAccess(reportId: string, sessionToken: string | null): { access: ReportAccess | null; linked: string } {
  const linked = new URLSearchParams(window.location.hash.slice(1)).get("token")?.trim() ?? "";
  if (linked) return { access: { kind: "share", token: linked }, linked };
  if (sessionToken) return { access: { kind: "session" }, linked: "" };
  try {
    const remembered = sessionStorage.getItem(`postdare.report.${reportId}`)?.trim();
    if (remembered) return { access: { kind: "share", token: remembered }, linked: "" };
  } catch {
    /* private storage can be unavailable */
  }
  return { access: null, linked: "" };
}

/** One report, read in a tab of its own. A notification hands this page to a
 *  reader who has no account, carrying its own token; the deploy task page
 *  opens the same page for the operator who is already signed in. */
export function ReportPage() {
  const { reportId = "" } = useParams();
  const sessionToken = useAuthStore((state) => state.token);
  // Pinned for the life of the mount: the link is spent in the effect below, and
  // a later render must not quietly fall back to the session just because the
  // address bar no longer carries the token.
  const [{ access, linked }] = useState(() => readReportAccess(reportId, sessionToken));
  // Spend the link once it has been read: out of the address bar so it stays out
  // of history and out of any referrer, kept for this tab so a refresh resolves.
  useEffect(() => {
    if (!linked) return;
    try {
      sessionStorage.setItem(`postdare.report.${reportId}`, linked);
    } catch {
      /* private storage can be unavailable */
    }
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
  }, [reportId, linked]);
  const report = useQuery({
    queryKey: ["report", reportId, access?.kind ?? "none", access?.kind === "share" ? access.token : ""],
    queryFn: () => (access?.kind === "share" ? getPublicReport(reportId, access.token) : getReport(reportId, sessionToken)),
    enabled: Boolean(reportId && access),
    retry: false
  });
  const data = report.data?.data;

  if (!access) {
    return <ReportMessage title="Report link is incomplete" body="Open the complete share link from your deployment notification, or sign in to Postdare to read the report." />;
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
      <Toaster />
    </main>
  );
}

function ReportMessage({ title, body }: { title: string; body: string }) {
  return <main className="grid min-h-dvh place-items-center p-5"><div className="w-full max-w-md border-y border-border py-8 text-center"><AlertTriangle className="mx-auto h-6 w-6 text-warning" /><h1 className="mt-3 text-lg font-semibold">{title}</h1><p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-muted">{body}</p></div></main>;
}

function ReportSkeleton() {
  return <main className="report-shell animate-pulse"><div className="h-20 border-b border-border bg-surface" /><div className="report-content"><div className="h-40 rounded-md bg-surface" /><div className="mt-8 h-72 rounded-md bg-surface" /></div></main>;
}
