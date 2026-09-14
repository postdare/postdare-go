import { useState } from "react";
import { useNavigate } from "react-router-dom";
import * as Dialog from "@radix-ui/react-dialog";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, ArchiveRestore, MoreHorizontal, Pencil, Trash2 } from "lucide-react";

import { deleteBoard, updateBoard } from "../../api/postdareGo";
import type { Board } from "../../api/types";
import { cn } from "../../lib/utils";
import { useAuthStore } from "../../store/auth";
import { toast } from "../../store/toast";
import { Button } from "../ui/button";
import { BoardSettingsDialog } from "./BoardSettingsDialog";

const itemClass =
  "flex h-8 cursor-pointer select-none items-center gap-2 rounded px-2 text-sm text-ink outline-none data-[highlighted]:bg-surface-2 data-[disabled]:opacity-45";

/** Everything you can do to a board rather than on it: rename it, retire it,
 *  or remove it. They live behind one menu because all three are rare next to
 *  filing an issue, which is what the header's other buttons are for. */
export function BoardMenu({ board }: { board: Board }) {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const archived = Boolean(board.archived_at);

  const archive = useMutation({
    mutationFn: (next: boolean) => updateBoard(board.id, { archived: next }, token),
    onSuccess: (_result, next) => {
      queryClient.invalidateQueries({ queryKey: ["board", String(board.id)] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      toast(next ? "Board archived" : "Board restored");
      // An archived board is gone from the index, so staying on it would leave
      // you somewhere you can no longer navigate back to.
      if (next) navigate("/boards");
    },
    onError: (error: Error) => toast(error.message, "danger")
  });

  const remove = useMutation({
    mutationFn: () => deleteBoard(board.id, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      toast("Board deleted");
      navigate("/boards");
    },
    onError: (error: Error) => {
      setConfirmingDelete(false);
      toast(error.message, "danger");
    }
  });

  return (
    <>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger asChild>
          <Button variant="ghost" size="sm" aria-label="Board menu">
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            sideOffset={6}
            className="z-50 min-w-[196px] overflow-hidden rounded-md border border-border bg-surface p-1 shadow-lg shadow-black/20"
          >
            <DropdownMenu.Item className={itemClass} onSelect={() => setEditing(true)}>
              <Pencil className="h-3.5 w-3.5 text-muted" aria-hidden />
              Board settings
            </DropdownMenu.Item>
            <DropdownMenu.Item className={itemClass} onSelect={() => archive.mutate(!archived)}>
              {archived ? (
                <ArchiveRestore className="h-3.5 w-3.5 text-muted" aria-hidden />
              ) : (
                <Archive className="h-3.5 w-3.5 text-muted" aria-hidden />
              )}
              {archived ? "Restore board" : "Archive board"}
            </DropdownMenu.Item>
            <DropdownMenu.Separator className="my-1 h-px bg-border" />
            <DropdownMenu.Item
              className={cn(itemClass, "text-danger data-[highlighted]:bg-danger/10")}
              onSelect={() => setConfirmingDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden />
              Delete board
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>

      {editing ? <BoardSettingsDialog board={board} open onClose={() => setEditing(false)} /> : null}

      <Dialog.Root open={confirmingDelete} onOpenChange={(next) => (next ? undefined : setConfirmingDelete(false))}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(420px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-surface p-4 shadow-xl focus:outline-none">
            <Dialog.Title className="text-sm font-semibold text-ink">Delete {board.name}?</Dialog.Title>
            <Dialog.Description className="mt-2 text-sm text-muted">
              The board and its key <span className="font-mono text-ink">{board.key}</span> are gone for good, and
              nothing will resolve {board.key}-1 afterwards. A board that still has issues cannot be deleted — archive
              it instead to keep them readable.
            </Dialog.Description>
            <div className="mt-4 flex justify-end gap-2">
              <Dialog.Close asChild>
                <Button variant="ghost" size="sm">
                  Cancel
                </Button>
              </Dialog.Close>
              <Button variant="danger" size="sm" disabled={remove.isPending} onClick={() => remove.mutate()}>
                {remove.isPending ? "Deleting…" : "Delete board"}
              </Button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
