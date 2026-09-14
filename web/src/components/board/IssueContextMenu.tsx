import * as ContextMenu from "@radix-ui/react-context-menu";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronRight, Copy, Hash, Link2, Trash2, Type } from "lucide-react";
import { useState, type ReactNode } from "react";

import { deleteIssue, updateIssue } from "../../api/postdareGo";
import type { Issue } from "../../api/types";
import { copyText } from "../../lib/clipboard";
import { cn } from "../../lib/utils";
import { useAuthStore } from "../../store/auth";
import { toast } from "../../store/toast";
import { DeleteIssueDialog } from "./DeleteIssueDialog";
import {
  ISSUE_PRIORITIES,
  ISSUE_STATUSES,
  PRIORITY_ICON,
  PRIORITY_LABELS,
  PRIORITY_TEXT,
  STATUS_ICON,
  STATUS_LABELS,
  STATUS_TEXT
} from "./boardMeta";

const contentClass =
  "z-50 min-w-[196px] overflow-hidden rounded-md border border-border bg-surface p-1 shadow-lg shadow-black/20";

const itemClass =
  "flex h-8 cursor-pointer select-none items-center gap-2 rounded px-2 text-sm text-ink outline-none data-[highlighted]:bg-surface-2 data-[disabled]:opacity-45";

const subTriggerClass = cn(itemClass, "data-[state=open]:bg-surface-2");

/** An issue's address. The route is keyed by id, so the link survives a rename
 *  of the issue or its board -- which is what makes it safe to paste into a
 *  ticket or a chat where it will be read much later. */
export function issueURL(issue: Issue) {
  return new URL(`/issues/${issue.id}`, window.location.origin).toString();
}

async function copy(value: string, confirmation: string) {
  const copied = await copyText(value);
  if (copied) {
    toast(confirmation);
    return;
  }
  toast("Could not copy to the clipboard", "danger");
}

function Submenu({ label, icon, children }: { label: string; icon: ReactNode; children: ReactNode }) {
  return (
    <ContextMenu.Sub>
      <ContextMenu.SubTrigger className={subTriggerClass}>
        {icon}
        {label}
        <ChevronRight className="ml-auto h-3.5 w-3.5 text-muted" aria-hidden />
      </ContextMenu.SubTrigger>
      <ContextMenu.Portal>
        <ContextMenu.SubContent className={contentClass} collisionPadding={8}>
          {children}
        </ContextMenu.SubContent>
      </ContextMenu.Portal>
    </ContextMenu.Sub>
  );
}

/** The right-click menu on a board card: what someone does to an issue without
 *  leaving the board -- copy it into a commit message or a chat, move it on, or
 *  drop it. Opening the issue stays the card's own click.
 *
 *  Copies and edits are grouped behind submenus so the first level stays a
 *  short list of verbs rather than a wall of items.
 *
 *  Radix opens this on the browser's context-menu gesture, which covers a
 *  right-click and a touch long-press; once it is open, arrow keys, Enter and
 *  Escape drive it and focus returns to the card.
 */
export function IssueContextMenu({ issue, children }: { issue: Issue; children: ReactNode }) {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // The board is the only place these show, and it renders from the same two
  // queries however it was reached, so the prefix is enough to refetch it
  // without threading the board id down to every card.
  function invalidateBoard() {
    queryClient.invalidateQueries({ queryKey: ["board-issues"] });
    queryClient.invalidateQueries({ queryKey: ["boards"] });
  }

  const edit = useMutation({
    mutationFn: (changes: Record<string, unknown>) => updateIssue(issue.id, changes, token),
    onError: (error) => toast(error.message, "danger"),
    onSettled: invalidateBoard
  });

  const remove = useMutation({
    mutationFn: () => deleteIssue(issue.id, token),
    onSuccess: () => toast(`Deleted ${issue.identifier}`),
    onError: (error) => toast(error.message, "danger"),
    onSettled: invalidateBoard
  });

  const StatusIcon = STATUS_ICON[issue.status];
  const PriorityIcon = PRIORITY_ICON[issue.priority];

  return (
    <>
      <ContextMenu.Root>
        <ContextMenu.Trigger asChild>{children}</ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content className={contentClass} collisionPadding={8}>
            <Submenu
              label="Status"
              icon={<StatusIcon className={cn("h-3.5 w-3.5", STATUS_TEXT[issue.status])} aria-hidden />}
            >
              {ISSUE_STATUSES.map((status) => {
                const Icon = STATUS_ICON[status];
                return (
                  <ContextMenu.Item
                    key={status}
                    className={itemClass}
                    disabled={status === issue.status}
                    onSelect={() => edit.mutate({ status })}
                  >
                    <Icon className={cn("h-3.5 w-3.5", STATUS_TEXT[status])} aria-hidden />
                    <span className="flex-1">{STATUS_LABELS[status]}</span>
                    {status === issue.status ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
                  </ContextMenu.Item>
                );
              })}
            </Submenu>

            <Submenu
              label="Priority"
              icon={<PriorityIcon className={cn("h-3.5 w-3.5", PRIORITY_TEXT[issue.priority])} aria-hidden />}
            >
              {ISSUE_PRIORITIES.map((priority) => {
                const ItemIcon = PRIORITY_ICON[priority];
                return (
                  <ContextMenu.Item
                    key={priority}
                    className={itemClass}
                    disabled={priority === issue.priority}
                    onSelect={() => edit.mutate({ priority })}
                  >
                    <ItemIcon className={cn("h-3.5 w-3.5", PRIORITY_TEXT[priority])} aria-hidden />
                    <span className="flex-1">{PRIORITY_LABELS[priority]}</span>
                    {priority === issue.priority ? <Check className="h-3.5 w-3.5 text-muted" aria-hidden /> : null}
                  </ContextMenu.Item>
                );
              })}
            </Submenu>

            <Submenu label="Copy" icon={<Copy className="h-3.5 w-3.5 text-muted" aria-hidden />}>
              <ContextMenu.Item
                className={itemClass}
                onSelect={() => void copy(issue.identifier, `Copied ${issue.identifier}`)}
              >
                <Hash className="h-3.5 w-3.5 text-muted" aria-hidden />
                Copy ID
              </ContextMenu.Item>
              <ContextMenu.Item className={itemClass} onSelect={() => void copy(issueURL(issue), "Copied issue URL")}>
                <Link2 className="h-3.5 w-3.5 text-muted" aria-hidden />
                Copy URL
              </ContextMenu.Item>
              <ContextMenu.Item className={itemClass} onSelect={() => void copy(issue.title, "Copied title")}>
                <Type className="h-3.5 w-3.5 text-muted" aria-hidden />
                Copy title
              </ContextMenu.Item>
            </Submenu>

            <ContextMenu.Separator className="my-1 h-px bg-border" />

            <ContextMenu.Item
              className={cn(itemClass, "text-danger data-[highlighted]:bg-danger/15")}
              // Opening the dialog in this same commit makes Radix register the
              // dialog's layer while the menu is still holding the body at
              // `pointer-events: none`, so the dialog captures that as the value
              // to put back and the closed board stays unclickable. One tick
              // later the menu has finished closing and the body is clean.
              onSelect={() => window.setTimeout(() => setConfirmingDelete(true), 0)}
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden />
              Delete issue
            </ContextMenu.Item>
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>

      {/* Outside the menu so it survives the menu closing, which is what
          selecting Delete does. */}
      <DeleteIssueDialog
        issue={issue}
        open={confirmingDelete}
        pending={remove.isPending}
        onCancel={() => setConfirmingDelete(false)}
        onConfirm={() => {
          setConfirmingDelete(false);
          remove.mutate();
        }}
      />
    </>
  );
}
