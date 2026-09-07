import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, FileSearch, GitCommitHorizontal, ShieldAlert } from "lucide-react";
import ReactMarkdown from "react-markdown";
import { useParams } from "react-router-dom";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { getPublicReport } from "../api/postdareGo";
import { highlightLines, languageForPath, type HighlightNode } from "../lib/diffHighlight";
import type { ReportIssue } from "../api/types";
import { Badge } from "../components/ui/badge";

// Display names for the report types the server can produce; keep in sync with
// model.ReportTypes(). An unknown type still renders, just without a name.
const reportTypeLabels: Record<string, string> = {
  ai_review: "Code review report",
};

const reportTypeLabel = (type: string) => reportTypeLabels[type] ?? "Report";

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
  const issues = data?.issues ?? [];
  const counts = countIssues(issues);

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
        <section className="report-overview" aria-labelledby="report-summary">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              {data.status === "success" ? <CheckCircle2 className="h-5 w-5 text-success" /> : <AlertTriangle className="h-5 w-5 text-warning" />}
              <h2 id="report-summary" className="text-base font-semibold">{data.conclusion || "Review result"}</h2>
            </div>
            <p className="mt-2 max-w-[72ch] text-sm leading-6 text-muted">{data.summary || data.error_message || "No summary was provided."}</p>
            <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
              <span>Deploy: {data.deploy_status}</span><span>Branch: {data.branch || "unknown"}</span><span>Task #{data.task_id}</span>
            </div>
          </div>
          <div className="risk-counts" aria-label="Issue counts">
            <RiskCount label="High" count={counts.high} tone="danger" />
            <RiskCount label="Medium" count={counts.medium} tone="warning" />
            <RiskCount label="Low" count={counts.low} tone="info" />
          </div>
        </section>

        {issues.length > 0 ? (
          <section className="report-section" aria-labelledby="report-issues">
            <h2 id="report-issues" className="report-section-title"><ShieldAlert className="h-4 w-4" /> Findings</h2>
            <div className="divide-y divide-border border-y border-border">
              {issues.map((issue, index) => <IssueRow key={`${issue.title}-${index}`} issue={issue} />)}
            </div>
          </section>
        ) : null}

        <section className="report-section" aria-labelledby="report-details">
          <h2 id="report-details" className="report-section-title">Full report</h2>
          <article className="report-markdown">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeSanitize]}
              components={{ code: MarkdownCode }}
            >
              {data.markdown || "_No detailed report was provided._"}
            </ReactMarkdown>
          </article>
        </section>
      </div>
    </main>
  );
}

function MarkdownCode({ className, children, ...props }: React.ComponentPropsWithoutRef<"code">) {
  const language = /language-(\w+)/.exec(className ?? "")?.[1];
  const source = String(children).replace(/\n$/, "");
  if (language === "mermaid") return <MermaidDiagram source={source} />;
  return <code className={className} {...props}>{children}</code>;
}

function MermaidDiagram({ source }: { source: string }) {
  const [svg, setSvg] = useState("");
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    void import("mermaid").then(async ({ default: mermaid }) => {
      mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: document.documentElement.classList.contains("dark") ? "dark" : "default" });
      const rendered = await mermaid.render(`report-mermaid-${crypto.randomUUID()}`, source);
      if (active) setSvg(rendered.svg);
    }).catch(() => active && setFailed(true));
    return () => { active = false; };
  }, [source]);
  if (failed) return <pre><code className="language-mermaid">{source}</code></pre>;
  if (!svg) return <div className="mermaid-loading">Rendering diagram…</div>;
  return <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: svg }} />;
}

function IssueRow({ issue }: { issue: ReportIssue }) {
  return (
    <article className="issue-row">
      <Badge tone={issue.severity === "high" ? "failed" : issue.severity === "medium" ? "pending" : "running"}>{issue.severity}</Badge>
      <div className="min-w-0">
        <h3 className="text-sm font-semibold">{issue.title}</h3>
        {/* With an excerpt the path sits on its header and the line in its gutter,
            so repeating "file:line" here would say it a third time. */}
        {issue.location && !issue.diff_hunk ? (
          <code className="mt-1 block break-all text-xs text-info">{issue.location}</code>
        ) : null}
        <dl className="mt-3 grid gap-2 text-sm sm:grid-cols-3">
          {issue.trigger ? <IssueDetail label="Trigger" value={issue.trigger} /> : null}
          {issue.impact ? <IssueDetail label="Impact" value={issue.impact} /> : null}
          {issue.suggestion ? <IssueDetail label="Suggestion" value={issue.suggestion} /> : null}
        </dl>
        {issue.diff_hunk ? <DiffHunk hunk={issue.diff_hunk} location={issue.location} /> : null}
      </div>
    </article>
  );
}

type DiffRow = {
  kind: "meta" | "add" | "del" | "context";
  oldNumber?: number;
  newNumber?: number;
  text: string;
};

// Walk a unified-diff hunk, numbering each side from the @@ header so the excerpt
// can be read against the file the way a diff view shows it. Lines that are not
// diff content -- the header, and the notes the server appends when an excerpt is
// cut or omitted -- carry no number.
function parseHunk(hunk: string): DiffRow[] {
  let oldNumber = 0;
  let newNumber = 0;
  return hunk.split("\n").map<DiffRow>((line) => {
    const header = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
    if (header) {
      oldNumber = Number(header[1]);
      newNumber = Number(header[2]);
      return { kind: "meta", text: line };
    }
    if (line.startsWith("+")) return { kind: "add", newNumber: newNumber++, text: line.slice(1) };
    if (line.startsWith("-")) return { kind: "del", oldNumber: oldNumber++, text: line.slice(1) };
    // A blank context line loses its leading space to any tool that strips trailing
    // whitespace; treating it as meta would desynchronise every number below it.
    if (line.startsWith(" ") || line === "") {
      return { kind: "context", oldNumber: oldNumber++, newNumber: newNumber++, text: line.slice(1) };
    }
    return { kind: "meta", text: line };
  });
}

const diffMarkers: Record<DiffRow["kind"], string> = { add: "+", del: "-", context: " ", meta: "" };

// "path/to/file.go:12" names the file the excerpt came from; the line is already
// on the rows, so the header carries only the path.
function excerptPath(location?: string) {
  return (location ?? "").replace(/:\d+.*$/, "").trim();
}

function HighlightedCode({ nodes }: { nodes: HighlightNode[] }) {
  return (
    <>
      {nodes.map((node, index) =>
        node.type === "text" ? (
          node.value
        ) : (
          <span key={index} className={node.className}>
            <HighlightedCode nodes={node.children} />
          </span>
        ),
      )}
    </>
  );
}

// Each side is highlighted as one document and then split by line, so a construct
// spanning several lines keeps one colour. A removed line belongs to the old side
// and an added line to the new one; context lines are in both.
function highlightRows(rows: DiffRow[], language?: string): (HighlightNode[] | null)[] {
  if (!language) return rows.map(() => null);
  const sides = {
    old: highlightLines(rows.filter((row) => row.kind === "del" || row.kind === "context").map((row) => row.text).join("\n"), language),
    new: highlightLines(rows.filter((row) => row.kind === "add" || row.kind === "context").map((row) => row.text).join("\n"), language),
  };
  const next = { old: 0, new: 0 };
  return rows.map((row) => {
    if (row.kind === "meta") return null;
    if (row.kind === "del") return sides.old[next.old++] ?? null;
    if (row.kind === "add") return sides.new[next.new++] ?? null;
    next.new += 1;
    return sides.old[next.old++] ?? null;
  });
}

function DiffHunk({ hunk, location }: { hunk: string; location?: string }) {
  const rows = parseHunk(hunk);
  const path = excerptPath(location);
  const highlighted = highlightRows(rows, languageForPath(path));
  // An excerpt the server omitted has no diff content to lay out; show the note.
  if (!rows.some((row) => row.kind !== "meta")) {
    return <p className="issue-diff-note">{hunk}</p>;
  }
  return (
    <div className="issue-diff">
      {path ? <div className="issue-diff-head">{path}</div> : null}
      <div className="issue-diff-body">
        <table>
          <tbody>
            {rows.map((row, index) => (
              <tr key={index} className={`diff-${row.kind}`}>
                {row.kind === "meta" ? (
                  <td className="diff-code" colSpan={4}>{row.text}</td>
                ) : (
                  <>
                    <td className="diff-num">{row.oldNumber ?? ""}</td>
                    <td className="diff-num">{row.newNumber ?? ""}</td>
                    <td className="diff-marker">{diffMarkers[row.kind]}</td>
                    <td className="diff-code">
                      {highlighted[index] ? <HighlightedCode nodes={highlighted[index]!} /> : row.text}
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function IssueDetail({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-xs font-medium text-muted">{label}</dt><dd className="mt-0.5 leading-5 text-ink">{value}</dd></div>;
}

function RiskCount({ label, count, tone }: { label: string; count: number; tone: string }) {
  const colors: Record<string, string> = { danger: "bg-danger", warning: "bg-warning", info: "bg-info" };
  return <div className="risk-count"><span className={`risk-dot ${colors[tone]}`} /><strong>{count}</strong><span>{label}</span></div>;
}

function countIssues(issues: ReportIssue[]) {
  return issues.reduce<Record<ReportIssue["severity"], number>>((counts, issue) => ({ ...counts, [issue.severity]: counts[issue.severity] + 1 }), { high: 0, medium: 0, low: 0 });
}

function shortCommit(value: string) { return value && value.length > 10 ? value.slice(0, 10) : value || "unknown"; }

function ReportMessage({ title, body }: { title: string; body: string }) {
  return <main className="grid min-h-dvh place-items-center p-5"><div className="w-full max-w-md border-y border-border py-8 text-center"><AlertTriangle className="mx-auto h-6 w-6 text-warning" /><h1 className="mt-3 text-lg font-semibold">{title}</h1><p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-muted">{body}</p></div></main>;
}

function ReportSkeleton() {
  return <main className="report-shell animate-pulse"><div className="h-20 border-b border-border bg-surface" /><div className="report-content"><div className="h-40 rounded-md bg-surface" /><div className="mt-8 h-72 rounded-md bg-surface" /></div></main>;
}
