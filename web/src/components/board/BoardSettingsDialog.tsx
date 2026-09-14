import { useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { listProjects, updateBoard } from "../../api/postdareGo";
import type { Board } from "../../api/types";
import { useAuthStore } from "../../store/auth";
import { toast } from "../../store/toast";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Textarea } from "../ui/textarea";

/** Edits the parts of a board that are safe to change afterwards: its name, what
 *  it tracks, and which project deploys it.
 *
 *  The key is not among them. It is baked into every issue identifier already
 *  written into a commit message or a chat thread, so it is shown here as the
 *  fixed fact it is rather than as a field that will not save.
 */
export function BoardSettingsDialog({ board, open, onClose }: { board: Board; open: boolean; onClose: () => void }) {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const [name, setName] = useState(board.name);
  const [description, setDescription] = useState(board.description ?? "");
  const [projectID, setProjectID] = useState(board.project_id ? String(board.project_id) : "");
  const [error, setError] = useState<string | undefined>();

  const projects = useQuery({ queryKey: ["projects"], queryFn: () => listProjects(token), enabled: open });

  const save = useMutation({
    mutationFn: () =>
      updateBoard(
        board.id,
        {
          name: name.trim(),
          description: description.trim(),
          // A board that was linked and is now "Not linked" needs the link
          // cleared, which a null project_id cannot say on its own.
          ...(projectID ? { project_id: Number(projectID) } : { clear_project: true })
        },
        token
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["board", String(board.id)] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      toast("Board saved");
      onClose();
    },
    onError: (mutationError: Error) => setError(mutationError.message)
  });

  return (
    <Dialog.Root open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(480px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-surface p-4 shadow-xl focus:outline-none">
          <Dialog.Title className="text-sm font-semibold text-ink">Board settings</Dialog.Title>
          <div className="mt-3 space-y-3">
            <div>
              <label className="text-xs font-medium text-muted" htmlFor="board-settings-name">
                Name
              </label>
              <Input
                id="board-settings-name"
                className="mt-1"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted" htmlFor="board-settings-project">
                Deploy project
              </label>
              <select
                id="board-settings-project"
                className="mt-1 h-9 w-full rounded-md border border-border bg-background px-2 text-sm text-ink outline-none transition-colors focus:border-primary focus:ring-2 focus:ring-primary/30"
                value={projectID}
                onChange={(event) => setProjectID(event.target.value)}
              >
                <option value="">Not linked</option>
                {(projects.data?.data ?? []).map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="text-xs font-medium text-muted" htmlFor="board-settings-description">
                Description
              </label>
              <Textarea
                id="board-settings-description"
                className="mt-1"
                value={description}
                placeholder="What this board tracks."
                onChange={(event) => setDescription(event.target.value)}
              />
            </div>
            <p className="text-xs text-muted">
              Key <span className="font-mono text-ink">{board.key}</span> is fixed: it names every issue on this board,
              including the ones already quoted in commits.
            </p>
            {error ? <p className="text-sm text-danger">{error}</p> : null}
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close asChild>
              <Button variant="ghost" size="sm">
                Cancel
              </Button>
            </Dialog.Close>
            <Button
              variant="primary"
              size="sm"
              disabled={save.isPending || name.trim() === ""}
              onClick={() => save.mutate()}
            >
              {save.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
