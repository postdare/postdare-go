import * as Dialog from "@radix-ui/react-dialog";
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Maximize2, Minimize2, X } from "lucide-react";

import { createIssue, listBoardLabels, listBoardUsers } from "../../api/postdareGo";
import { useQuery } from "@tanstack/react-query";
import type { IssuePriority, IssueStatus } from "../../api/types";
import { IssueDescriptionField } from "./IssueDescription";
import { AssigneePicker, PriorityPicker, StatusPicker } from "./IssueProperties";
import { LabelsEditor } from "./LabelsEditor";
import { Button } from "../ui/button";
import { cn } from "../../lib/utils";
import { useAuthStore } from "../../store/auth";

/** The quick composer: a light modal for filing an issue without leaving the
 *  board, with an expand control that hands the same draft to the full page for
 *  anything that needs room -- a long description, several screenshots. */
export function IssueComposer({
  open,
  boardID,
  boardKey,
  defaultStatus,
  onClose,
  onCreated
}: {
  open: boolean;
  boardID: string;
  boardKey?: string;
  defaultStatus: IssueStatus;
  onClose: () => void;
  onCreated: () => void;
}) {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: ["board-users"], queryFn: () => listBoardUsers(token) });
  const boardLabels = useQuery({ queryKey: ["board-labels", boardID], queryFn: () => listBoardLabels(boardID, token) });

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<IssueStatus>(defaultStatus);
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [assigneeID, setAssigneeID] = useState<number | null>(null);
  const [labels, setLabels] = useState<string[]>([]);
  // Filing a batch of issues is the normal way a board gets seeded, so the
  // composer can stay open and reset instead of closing after each one.
  const [createMore, setCreateMore] = useState(false);
  // Expanding grows the composer in place rather than moving the draft to
  // another screen: what is being written is the same issue either way, and a
  // long description should not cost a navigation to start typing.
  const [expanded, setExpanded] = useState(false);

  function reset() {
    setTitle("");
    setDescription("");
    setPriority("none");
    setAssigneeID(null);
    setLabels([]);
  }

  const create = useMutation({
    mutationFn: () =>
      createIssue(
        boardID,
        {
          title: title.trim(),
          description,
          status,
          priority,
          labels,
          ...(assigneeID === null ? { clear_assignee: true } : { assignee_id: assigneeID })
        },
        token
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      queryClient.invalidateQueries({ queryKey: ["board-labels"] });
      onCreated();
      reset();
      if (!createMore) onClose();
    }
  });

  return (
    <Dialog.Root open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-30 bg-black/50" />
        <Dialog.Content
          className={cn(
            "scrollbar-subtle fixed inset-x-0 bottom-0 z-40 flex max-h-[92dvh] flex-col rounded-t-xl border border-border bg-surface shadow-xl focus:outline-none sm:inset-x-auto sm:bottom-auto sm:left-1/2 sm:-translate-x-1/2 sm:rounded-xl",
            expanded
              ? "sm:top-[4vh] sm:h-[92vh] sm:w-[min(1100px,calc(100vw-4rem))]"
              : "sm:top-[12vh] sm:w-[min(720px,calc(100vw-2rem))]"
          )}
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          <div className="flex shrink-0 items-center justify-between gap-2 px-4 pt-3">
            <Dialog.Title className="flex min-w-0 items-center gap-1.5 text-xs text-muted">
              {boardKey ? (
                <span className="rounded border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-ink">
                  {boardKey}
                </span>
              ) : null}
              <span aria-hidden>›</span>
              <span>New issue</span>
            </Dialog.Title>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 px-0"
                aria-label={expanded ? "Collapse composer" : "Expand composer"}
                title={expanded ? "Collapse" : "Expand"}
                onClick={() => setExpanded((current) => !current)}
              >
                {expanded ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
              </Button>
              <Dialog.Close asChild>
                <Button variant="ghost" size="sm" className="h-7 w-7 px-0" aria-label="Close">
                  <X className="h-3.5 w-3.5" />
                </Button>
              </Dialog.Close>
            </div>
          </div>
          <Dialog.Description className="sr-only">
            File a new issue on this board.
          </Dialog.Description>

          {/* The writing area takes whatever height is left, so expanding gives
              the description the room rather than pushing the controls away. */}
          <div className="scrollbar-subtle flex min-h-0 flex-1 flex-col overflow-y-auto px-4 pt-2">
            {/* Borderless, so the composer reads as a document being written
                rather than a form being filled in. */}
            <input
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              placeholder="Issue title"
              aria-label="Issue title"
              autoFocus
              className="w-full bg-transparent text-lg font-medium text-ink outline-none placeholder:text-muted"
              onKeyDown={(event) => {
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey) && title.trim()) create.mutate();
              }}
            />
            <div className="mt-1 flex min-h-0 flex-1 flex-col">
              <IssueDescriptionField
                value={description}
                placeholder="Add description…"
                onChange={setDescription}
                bare
                grow={expanded}
              />
            </div>
          </div>

          <div className="flex shrink-0 flex-wrap items-center gap-1.5 px-4 pt-3">
            <StatusPicker value={status} onChange={setStatus} />
            <PriorityPicker value={priority} onChange={setPriority} />
            <AssigneePicker value={assigneeID} users={users.data?.data ?? []} onChange={setAssigneeID} />
            <LabelsEditor layout="pill" value={labels} suggestions={boardLabels.data?.data ?? []} onChange={setLabels} />
          </div>

          {create.error ? <p className="shrink-0 px-4 pt-2 text-sm text-danger">{create.error.message}</p> : null}

          <div className="mt-3 flex shrink-0 items-center justify-end gap-2 border-t border-border px-4 py-3">
            <div className="flex items-center gap-3">
              <label className="inline-flex cursor-pointer items-center gap-2 text-xs text-muted">
                <input
                  type="checkbox"
                  className="h-3.5 w-3.5 accent-primary"
                  checked={createMore}
                  onChange={(event) => setCreateMore(event.target.checked)}
                />
                Create more
              </label>
              <Button
                variant="primary"
                size="sm"
                disabled={create.isPending || title.trim() === ""}
                onClick={() => create.mutate()}
              >
                {create.isPending ? "Creating…" : "Create issue"}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
