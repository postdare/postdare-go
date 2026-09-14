import * as Dialog from "@radix-ui/react-dialog";

import type { Issue } from "../../api/types";
import { Button } from "../ui/button";

/** Confirms deleting one issue from the board.
 *
 *  Deleting takes the description and any screenshots pasted into it with it,
 *  and the API has no undo, so the card's menu asks here rather than acting on
 *  a mis-aimed click -- unlike the issue page, where Delete is a deliberate
 *  second step of its own.
 */
export function DeleteIssueDialog({
  issue,
  open,
  pending,
  onCancel,
  onConfirm
}: {
  issue: Issue;
  open: boolean;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => (next ? undefined : onCancel())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(420px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-surface p-4 shadow-xl focus:outline-none">
          <Dialog.Title className="text-sm font-semibold text-ink">Delete {issue.identifier}?</Dialog.Title>
          <Dialog.Description className="mt-2 text-sm text-muted">
            <span className="text-ink">“{issue.title}”</span> is removed from the board, together with its
            description and any images in it. This cannot be undone.
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </Dialog.Close>
            <Button variant="danger" size="sm" onClick={onConfirm} disabled={pending}>
              {pending ? "Deleting…" : "Delete issue"}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
