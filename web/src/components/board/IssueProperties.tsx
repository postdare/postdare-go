import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import type { ReactNode } from "react";
import { Check, User } from "lucide-react";

import type { BoardUser, IssuePriority, IssueStatus } from "../../api/types";
import { cn } from "../../lib/utils";
import {
  ISSUE_PRIORITIES,
  ISSUE_STATUSES,
  PRIORITY_BARS,
  PRIORITY_LABELS,
  PRIORITY_TEXT,
  STATUS_ICON,
  STATUS_LABELS,
  STATUS_TEXT
} from "./boardMeta";

/** "pill" is the compact row under the composer's description; "row" is the
 *  full-width control in the expanded page's sidebar. Same pickers either way,
 *  so a property is set the same way in both places. */
export type PropertyLayout = "pill" | "row";

const triggerClass = (layout: PropertyLayout, active: boolean) =>
  cn(
    "inline-flex h-8 items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 text-xs text-ink transition-colors hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 data-[state=open]:bg-surface-2",
    layout === "row" &&
      "h-8 w-full justify-start rounded-md border-transparent bg-transparent px-1.5 text-sm hover:bg-surface-2",
    !active && "text-muted"
  );

const menuClass =
  "z-50 min-w-[200px] overflow-hidden rounded-md border border-border bg-surface p-1 shadow-lg shadow-black/20";

const itemClass =
  "flex h-8 cursor-pointer select-none items-center gap-2 rounded px-2 text-sm text-ink outline-none data-[highlighted]:bg-surface-2";

function PriorityBars({ priority }: { priority: IssuePriority }) {
  const filled = PRIORITY_BARS[priority] ?? 0;
  return (
    <span className={cn("inline-flex items-end gap-[2px]", PRIORITY_TEXT[priority])} aria-hidden>
      {[3, 6, 9].map((height, index) => (
        <span
          key={height}
          className={cn("w-[3px] rounded-[1px]", index < filled ? "bg-current" : "bg-current/25")}
          style={{ height }}
        />
      ))}
    </span>
  );
}

function Picker({
  layout,
  active,
  trigger,
  children
}: {
  layout: PropertyLayout;
  active: boolean;
  trigger: ReactNode;
  children: ReactNode;
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className={triggerClass(layout, active)}>{trigger}</DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className={menuClass} align="start" sideOffset={4}>
          {children}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

export function StatusPicker({
  value,
  onChange,
  layout = "pill"
}: {
  value: IssueStatus;
  onChange: (next: IssueStatus) => void;
  layout?: PropertyLayout;
}) {
  const Icon = STATUS_ICON[value];
  return (
    <Picker
      layout={layout}
      active
      trigger={
        <>
          <Icon className={cn("h-3.5 w-3.5", STATUS_TEXT[value])} aria-hidden />
          <span>{STATUS_LABELS[value]}</span>
        </>
      }
    >
      {ISSUE_STATUSES.map((status) => {
        const StatusIcon = STATUS_ICON[status];
        return (
          <DropdownMenu.Item key={status} className={itemClass} onSelect={() => onChange(status)}>
            <StatusIcon className={cn("h-3.5 w-3.5", STATUS_TEXT[status])} aria-hidden />
            <span className="flex-1">{STATUS_LABELS[status]}</span>
            {status === value ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
          </DropdownMenu.Item>
        );
      })}
    </Picker>
  );
}

export function PriorityPicker({
  value,
  onChange,
  layout = "pill"
}: {
  value: IssuePriority;
  onChange: (next: IssuePriority) => void;
  layout?: PropertyLayout;
}) {
  return (
    <Picker
      layout={layout}
      active={value !== "none"}
      trigger={
        <>
          <PriorityBars priority={value} />
          <span>{value === "none" ? "Priority" : PRIORITY_LABELS[value]}</span>
        </>
      }
    >
      {ISSUE_PRIORITIES.map((priority) => (
        <DropdownMenu.Item key={priority} className={itemClass} onSelect={() => onChange(priority)}>
          <PriorityBars priority={priority} />
          <span className="flex-1">{PRIORITY_LABELS[priority]}</span>
          {priority === value ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
        </DropdownMenu.Item>
      ))}
    </Picker>
  );
}

export function AssigneePicker({
  value,
  users,
  onChange,
  layout = "pill"
}: {
  value: number | null;
  users: BoardUser[];
  onChange: (next: number | null) => void;
  layout?: PropertyLayout;
}) {
  const selected = users.find((user) => user.id === value);
  return (
    <Picker
      layout={layout}
      active={Boolean(selected)}
      trigger={
        <>
          <User className="h-3.5 w-3.5" aria-hidden />
          <span>{selected?.username ?? "Assignee"}</span>
        </>
      }
    >
      <DropdownMenu.Item className={itemClass} onSelect={() => onChange(null)}>
        <User className="h-3.5 w-3.5 text-muted" aria-hidden />
        <span className="flex-1">Unassigned</span>
        {value === null ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
      </DropdownMenu.Item>
      {users.map((user) => (
        <DropdownMenu.Item key={user.id} className={itemClass} onSelect={() => onChange(user.id)}>
          <span
            aria-hidden
            className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-surface-2 text-[9px] uppercase text-ink"
          >
            {user.username.slice(0, 1)}
          </span>
          <span className="flex-1">{user.username}</span>
          {user.id === value ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
        </DropdownMenu.Item>
      ))}
    </Picker>
  );
}
