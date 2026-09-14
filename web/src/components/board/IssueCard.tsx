import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GitBranch, MessageSquare } from "lucide-react";

import type { Issue } from "../../api/types";
import { cn } from "../../lib/utils";
import { IssueContextMenu } from "./IssueContextMenu";
import { LabelChip } from "./LabelsEditor";
import { PRIORITY_ICON, PRIORITY_LABELS, PRIORITY_TEXT } from "./boardMeta";

/** The card's priority mark. An unset priority draws nothing: the card is not a
 *  control, "no priority" is the default state of most cards, and a glyph for
 *  every one of them would be noise on the board. The picker, which does have
 *  to show the state it is in, carries a dash for that case. */
function PriorityMark({ priority }: { priority: Issue["priority"] }) {
  const Icon = PRIORITY_ICON[priority];
  if (priority === "none") return null;
  return (
    // `relative` is load-bearing: sr-only is position:absolute, and without a
    // positioned ancestor its containing block is the page itself, so the label
    // escapes the board's horizontal clip and gives the whole page a sideways
    // scrollbar on a phone.
    <span className={cn("relative inline-flex", PRIORITY_TEXT[priority])} title={PRIORITY_LABELS[priority]}>
      <span className="sr-only">{PRIORITY_LABELS[priority]}</span>
      <Icon className="h-3.5 w-3.5" aria-hidden />
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
        {issue.comment_count ? (
          // A card says a conversation is happening on it, not what was said:
          // the count is the part that is readable at board size.
          <span className="inline-flex shrink-0 items-center gap-1" title={`${issue.comment_count} comments`}>
            <MessageSquare className="h-3 w-3" aria-hidden />
            {issue.comment_count}
          </span>
        ) : null}
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
      {/* The whole card is the drag handle, the open trigger and the
          right-click target: dnd-kit only starts a drag past a small activation
          distance and only on the primary button, so a plain click still reads
          as a click and a right-click opens the copy menu instead. A button
          keeps the card reachable from the keyboard. */}
      <IssueContextMenu issue={issue}>
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
      </IssueContextMenu>
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
