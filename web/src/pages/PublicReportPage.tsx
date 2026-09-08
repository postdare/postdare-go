import { isValidElement, useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, FileSearch, GitCommitHorizontal, Maximize2, Minus, Plus, ShieldAlert, X } from "lucide-react";
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
              components={{ code: MarkdownCode, pre: MarkdownPre }}
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

// A diagram renders as a <figure>, which cannot live inside the <pre> that
// react-markdown wraps a fenced block in; unwrap the <pre> for those blocks.
function MarkdownPre({ children, ...props }: React.ComponentPropsWithoutRef<"pre">) {
  const child = Array.isArray(children) ? children[0] : children;
  const className = isValidElement<{ className?: string }>(child) ? child.props.className ?? "" : "";
  if (/\blanguage-mermaid\b/.test(className)) return <>{children}</>;
  return <pre {...props}>{children}</pre>;
}

function MermaidDiagram({ source }: { source: string }) {
  const [svg, setSvg] = useState("");
  const [failed, setFailed] = useState(false);
  const [expanded, setExpanded] = useState(false);
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
  return (
    <figure className="mermaid-figure">
      <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: svg }} />
      <button type="button" className="mermaid-expand" onClick={() => setExpanded(true)} title="Full screen" aria-label="Full screen">
        <Maximize2 className="h-4 w-4" aria-hidden="true" />
      </button>
      {expanded ? <MermaidViewer svg={svg} onClose={() => setExpanded(false)} /> : null}
    </figure>
  );
}

const zoomRange = { min: 0.4, max: 8 };
const clampZoom = (scale: number) => Math.min(zoomRange.max, Math.max(zoomRange.min, scale));
const identityView = { scale: 1, x: 0, y: 0 };

// A full-viewport view of one diagram: wheel or the buttons zoom, dragging pans,
// and Escape closes it -- a native <dialog> gives us the top layer and that key.
function MermaidViewer({ svg, onClose }: { svg: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ pointerId: number; x: number; y: number } | null>(null);
  const [view, setView] = useState(identityView);

  useEffect(() => { if (!dialogRef.current?.open) dialogRef.current?.showModal(); }, []);
  // The wheel listener has to be non-passive to keep the page behind from
  // scrolling, which rules out React's own onWheel.
  useEffect(() => {
    const stage = stageRef.current;
    if (!stage) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      setView((current) => ({ ...current, scale: clampZoom(current.scale * (event.deltaY < 0 ? 1.12 : 1 / 1.12)) }));
    };
    stage.addEventListener("wheel", onWheel, { passive: false });
    return () => stage.removeEventListener("wheel", onWheel);
  }, []);

  const zoomBy = useCallback((factor: number) => setView((current) => ({ ...current, scale: clampZoom(current.scale * factor) })), []);

  return (
    <dialog ref={dialogRef} className="mermaid-modal" onClose={onClose}>
      <div className="mermaid-modal-bar">
        <span className="text-xs text-muted">Drag to pan · scroll to zoom</span>
        <div className="mermaid-modal-actions">
          <button type="button" onClick={() => zoomBy(1 / 1.25)} aria-label="Zoom out"><Minus className="h-4 w-4" /></button>
          <span className="mermaid-zoom-value">{Math.round(view.scale * 100)}%</span>
          <button type="button" onClick={() => zoomBy(1.25)} aria-label="Zoom in"><Plus className="h-4 w-4" /></button>
          <button type="button" onClick={() => setView(identityView)}>Reset</button>
          <button type="button" onClick={() => dialogRef.current?.close()} aria-label="Close"><X className="h-4 w-4" /></button>
        </div>
      </div>
      <div
        ref={stageRef}
        className="mermaid-modal-stage"
        onPointerDown={(event) => {
          drag.current = { pointerId: event.pointerId, x: event.clientX - view.x, y: event.clientY - view.y };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          const from = drag.current;
          if (!from || from.pointerId !== event.pointerId) return;
          setView((current) => ({ ...current, x: event.clientX - from.x, y: event.clientY - from.y }));
        }}
        onPointerUp={() => { drag.current = null; }}
        onPointerCancel={() => { drag.current = null; }}
        onDoubleClick={() => setView(identityView)}
      >
        <div
          className="mermaid-modal-canvas"
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
          dangerouslySetInnerHTML={{ __html: svg }}
        />
      </div>
    </dialog>
  );
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
        <IssueFacts issue={issue} />
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

// Trigger, impact and suggestion read as one table with a header row: the three
// of them tell one story about the finding, so they belong side by side. A field
// the reviewer left out drops its whole column rather than heading an empty cell.
function IssueFacts({ issue }: { issue: ReportIssue }) {
  const facts = [
    { label: "Trigger", value: issue.trigger },
    { label: "Impact", value: issue.impact },
    { label: "Suggestion", value: issue.suggestion },
  ].filter((fact): fact is { label: string; value: string } => Boolean(fact.value));
  if (facts.length === 0) return null;
  return (
    <table className="issue-facts">
      <thead>
        <tr>{facts.map((fact) => <th key={fact.label} scope="col">{fact.label}</th>)}</tr>
      </thead>
      <tbody>
        {/* Narrow screens hide the header row and label each cell from data-label. */}
        <tr>{facts.map((fact) => <td key={fact.label} data-label={fact.label}>{fact.value}</td>)}</tr>
      </tbody>
    </table>
  );
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
