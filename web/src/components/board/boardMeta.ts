import { CheckCircle2, Circle, CircleDashed, CircleDot, XCircle, type LucideIcon } from "lucide-react";

import type { IssuePriority, IssueStatus } from "../../api/types";

/** The columns, in the order the board reads. The server owns this list; the
 *  constant is the fallback so the board still renders before /issues/meta
 *  answers, and so the column order never depends on map iteration. */
export const ISSUE_STATUSES: IssueStatus[] = ["backlog", "todo", "in_progress", "done", "canceled"];

export const STATUS_LABELS: Record<IssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  done: "Done",
  canceled: "Canceled"
};

/** Column accents double as the status dot. Status is never carried by colour
 *  alone — every column and card shows the status in text beside it. */
export const STATUS_DOT: Record<IssueStatus, string> = {
  backlog: "bg-muted",
  todo: "bg-info",
  in_progress: "bg-warning",
  done: "bg-success",
  canceled: "bg-muted"
};

/** The glyph a status carries in the pickers, so a column is recognisable
 *  before its label is read -- and still labelled, never colour alone. */
export const STATUS_ICON: Record<IssueStatus, LucideIcon> = {
  backlog: CircleDashed,
  todo: Circle,
  in_progress: CircleDot,
  done: CheckCircle2,
  canceled: XCircle
};

export const STATUS_TEXT: Record<IssueStatus, string> = {
  backlog: "text-muted",
  todo: "text-info",
  in_progress: "text-warning",
  done: "text-success",
  canceled: "text-muted"
};

export const ISSUE_PRIORITIES: IssuePriority[] = ["urgent", "high", "medium", "low", "none"];

export const PRIORITY_LABELS: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority"
};

export const PRIORITY_TEXT: Record<IssuePriority, string> = {
  urgent: "text-danger",
  high: "text-warning",
  medium: "text-info",
  low: "text-muted",
  none: "text-muted"
};

/** Filled bars out of three, so priority is legible without relying on colour. */
export const PRIORITY_BARS: Record<IssuePriority, number> = {
  urgent: 3,
  high: 3,
  medium: 2,
  low: 1,
  none: 0
};

export function isIssueStatus(value: string): value is IssueStatus {
  return (ISSUE_STATUSES as string[]).includes(value);
}
