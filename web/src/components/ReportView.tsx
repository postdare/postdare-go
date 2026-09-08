import { isValidElement, useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, CheckCircle2, Maximize2, Minus, Plus, ShieldAlert, X } from "lucide-react";
import ReactMarkdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import type { Report, ReportIssue } from "../api/types";
import { Badge } from "./ui/badge";
import { highlightLines, languageForPath, type HighlightNode } from "../lib/diffHighlight";

// Display names for the report types the server can produce; keep in sync with
// model.ReportTypes(). An unknown type still renders, just without a name.
const reportTypeLabels: Record<string, string> = {
  ai_review: "Code review report",
};

export const reportTypeLabel = (type: string) => reportTypeLabels[type] ?? "Report";

// A review script picks its own conclusion, and ours emit machine values like
// "issues_found". Give the ones we ship a written label and unslug the rest so a
// custom script's value still reads as a sentence rather than a field name.
const conclusionLabels: Record<string, string> = {
  passed: "No issues found",
  issues_found: "Issues found",
};

function conclusionLabel(conclusion: string) {
  if (!conclusion) return "Review result";
  return conclusionLabels[conclusion] ?? conclusion.replace(/_/g, " ").replace(/^./, (first) => first.toUpperCase());
}

// ReportBody is the report itself: verdict, findings and full markdown. The
// public page wraps it in its own shell; the deploy task page embeds it in a
// card, so nothing page-specific (header, token handling) lives here.
export function ReportBody({ report }: { report: Report }) {
  const issues = report.issues ?? [];
  const counts = countIssues(issues);
  // A run can succeed and still report findings, so a green tick is only right
  // when the review came back with nothing to fix.
  const verdict = report.status !== "success" ? "failed" : counts.high + counts.medium + counts.low > 0 ? "attention" : "clear";

  return (
    <>
      <section className="report-overview" aria-labelledby="report-summary">
        {/* The verdict and the counts are both short, so they share the top line;
            the summary then runs under them at a readable measure instead of
            leaving a hole beside the counts. */}
        <div className="report-verdict">
          <h2 id="report-summary">
            {verdict === "clear" ? <CheckCircle2 className="h-5 w-5 flex-none text-success" /> : <AlertTriangle className={`h-5 w-5 flex-none ${verdict === "failed" ? "text-danger" : "text-warning"}`} />}
            {conclusionLabel(report.conclusion)}
          </h2>
          <div className="risk-counts" aria-label="Issue counts">
            <RiskCount label="High" count={counts.high} tone="danger" />
            <RiskCount label="Medium" count={counts.medium} tone="warning" />
            <RiskCount label="Low" count={counts.low} tone="info" />
          </div>
        </div>
        <p className="report-summary-text">{report.summary || report.error_message || "No summary was provided."}</p>
        <dl className="report-meta">
          <div><dt>Deploy</dt><dd>{report.deploy_status}</dd></div>
          <div><dt>Branch</dt><dd>{report.branch || "unknown"}</dd></div>
          <div><dt>Task</dt><dd>#{report.task_id}</dd></div>
        </dl>
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
            {report.markdown || "_No detailed report was provided._"}
          </ReactMarkdown>
        </article>
      </section>
    </>
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
      {/* The severity sits on the title line: in its own column it left a tall
          empty gutter beside everything below the badge. */}
      <div className="issue-head">
        <Badge tone={issue.severity === "high" ? "failed" : issue.severity === "medium" ? "pending" : "running"}>{issue.severity}</Badge>
        <h3>{issue.title}</h3>
      </div>
      {/* With an excerpt the path sits on its header and the line in its gutter,
          so repeating "file:line" here would say it a third time. */}
      {issue.location && !issue.diff_hunk ? (
        <code className="mt-1 block break-all text-xs text-info">{issue.location}</code>
      ) : null}
      <IssueFacts issue={issue} />
      {issue.diff_hunk ? <DiffHunk hunk={issue.diff_hunk} location={issue.location} /> : null}
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
  return (
    <div className={`risk-count${count === 0 ? " risk-count-empty" : ""}`}>
      <span className={`risk-dot ${colors[tone]}`} /><strong>{count}</strong><span>{label}</span>
    </div>
  );
}

function countIssues(issues: ReportIssue[]) {
  return issues.reduce<Record<ReportIssue["severity"], number>>((counts, issue) => ({ ...counts, [issue.severity]: counts[issue.severity] + 1 }), { high: 0, medium: 0, low: 0 });
}
