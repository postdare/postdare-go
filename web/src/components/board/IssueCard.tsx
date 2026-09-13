import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GitBranch, Rocket } from "lucide-react";

import type { Issue } from "../../api/types";
import { cn } from "../../lib/utils";
import { LabelChip } from "./LabelsEditor";
import { PRIORITY_BARS, PRIORITY_LABELS, PRIORITY_TEXT } from "./boardMeta";

function PriorityMark({ priority }: { priority: Issue["priority"] }) {
  const filled = PRIORITY_BARS[priority] ?? 0;
  return (
    // `relative` is load-bearing: sr-only is position:absolute, and without a
    // positioned ancestor its containing block is the page itself, so the label
    // escapes the board's horizontal clip and gives the whole page a sideways
    // scrollbar on a phone.
    <span className={cn("relative inline-flex items-end gap-[2px]", PRIORITY_TEXT[priority])} title={PRIORITY_LABELS[priority]}>
      <span className="sr-only">{PRIORITY_LABELS[priority]}</span>
      {[3, 6, 9].map((height, index) => (
        <span
          key={height}
          aria-hidden
          className={cn("w-[3px] rounded-[1px]", index < filled ? "bg-current" : "bg-current/25")}
          style={{ height }}
        />
      ))}
    </span>
  );
}

/** The release strip: whether this issue has shipped, and how it went. It is the
 *  reason this board lives next to the deploy console rather than in Linear. */
function DeployMark({ issue }: { issue: Issue }) {
  const link = issue.deploy_links[0];
  if (!link) return null;
  const tone =
    link.status === "success"
      ? "text-success"
      : link.status === "failed"
        ? "text-danger"
        : link.status === "running" || link.status === "pending"
          ? "text-info"
          : "text-muted";
  return (
    <span className={cn("inline-flex items-center gap-1", tone)} title={`Deploy #${link.task_id} — ${link.status}`}>
      <Rocket className="h-3 w-3" aria-hidden />
      <span className="text-[11px] font-medium">{link.status}</span>
    </span>
  );
}

export function IssueCardBody({ issue, dragging }: { issue: Issue; dragging?: boolean }) {
  return (
    <div
      className={cn(
        "rounded-lg border border-border bg-surface p-3 text-left transition-colors",
        dragging ? "shadow-lg shadow-black/20" : "hover:border-primary/40"
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-[11px] text-muted">{issue.identifier}</span>
        <PriorityMark priority={issue.priority} />
      </div>
      <div className="mt-1.5 line-clamp-3 break-words text-sm text-ink">{issue.title}</div>
      {issue.labels.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1">
          {issue.labels.map((label) => (
            <LabelChip key={label} label={label} className="h-5 text-[11px]" />
          ))}
        </div>
      ) : null}
      <div className="mt-2 flex items-center justify-between gap-2 text-[11px] text-muted">
        <span className="inline-flex min-w-0 items-center gap-1">
          {issue.assignee_name ? (
            <>
              <span
                aria-hidden
                className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-surface-2 text-[9px] uppercase text-ink"
              >
                {issue.assignee_name.slice(0, 1)}
              </span>
              <span className="truncate">{issue.assignee_name}</span>
            </>
          ) : (
            <span className="text-muted">Unassigned</span>
          )}
        </span>
        <DeployMark issue={issue} />
      </div>
    </div>
  );
}

export function SortableIssueCard({ issue, onOpen }: { issue: Issue; onOpen: (issue: Issue) => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: issue.id,
    data: { status: issue.status }
  });

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn("touch-none", isDragging && "opacity-40")}
    >
      {/* The whole card is the drag handle and the open trigger: dnd-kit only
          starts a drag past a small activation distance, so a plain click still
          reads as a click. A button keeps it reachable from the keyboard. */}
      <button
        type="button"
        className="block w-full rounded-lg text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65"
        onClick={() => onOpen(issue)}
        aria-label={`${issue.identifier}: ${issue.title}`}
        {...attributes}
        {...listeners}
      >
        <IssueCardBody issue={issue} />
      </button>
    </div>
  );
}

export function IssueDragPreview({ issue }: { issue: Issue }) {
  return (
    <div className="w-[272px] rotate-1">
      <IssueCardBody issue={issue} dragging />
    </div>
  );
}

export function IssueBranchHint({ identifier }: { identifier: string }) {
  return (
    <span className="inline-flex items-center gap-1 font-mono text-xs text-muted">
      <GitBranch className="h-3 w-3" aria-hidden />
      fix {identifier}
    </span>
  );
}
