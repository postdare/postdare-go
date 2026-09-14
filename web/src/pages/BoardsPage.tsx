import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KanbanSquare, Plus } from "lucide-react";

import { createBoard, listBoards, listProjects } from "../api/postdareGo";
import { PageHeader } from "../components/PageHeader";
import { ISSUE_STATUSES, STATUS_LABELS } from "../components/board/boardMeta";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Textarea } from "../components/ui/textarea";
import { cn } from "../lib/utils";
import { useAuthStore } from "../store/auth";

/** Derives ENG from "Engineering" so the key field is usually already right --
 *  it is permanent once the board exists, since it is baked into every issue
 *  identifier people have already pasted into commits. */
function suggestKey(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const raw = words.length > 1 ? words.map((word) => word[0]).join("") : words[0] ?? "";
  return raw.replace(/[^A-Za-z0-9]/g, "").slice(0, 10).toUpperCase();
}

export function BoardsPage() {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [projectID, setProjectID] = useState<string>("");
  const [error, setError] = useState<string | undefined>();
  const [showArchived, setShowArchived] = useState(false);

  const boards = useQuery({
    queryKey: ["boards", showArchived ? "archived" : "active"],
    queryFn: () => listBoards(token, { archived: showArchived })
  });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => listProjects(token) });

  const create = useMutation({
    mutationFn: () =>
      createBoard(
        {
          name: name.trim(),
          key: key.trim().toUpperCase(),
          description: description.trim(),
          project_id: projectID ? Number(projectID) : null
        },
        token
      ),
    onSuccess: () => {
      setCreating(false);
      setName("");
      setKey("");
      setKeyTouched(false);
      setDescription("");
      setProjectID("");
      setError(undefined);
      queryClient.invalidateQueries({ queryKey: ["boards"] });
    },
    onError: (mutationError: Error) => setError(mutationError.message)
  });

  const rows = boards.data?.data ?? [];

  return (
    <>
      <PageHeader
        title="Boards"
        description="Issues grouped into boards. A board is a stream of work; a project is a deployable service."
        actions={
          <>
            <div className="inline-flex rounded-md border border-border p-0.5" role="group" aria-label="Board view">
              {[
                { label: "Active", archived: false },
                { label: "Archived", archived: true }
              ].map((view) => (
                <button
                  key={view.label}
                  type="button"
                  aria-pressed={showArchived === view.archived}
                  onClick={() => setShowArchived(view.archived)}
                  className={cn(
                    "h-8 rounded px-2.5 text-xs transition-colors",
                    showArchived === view.archived ? "bg-surface-2 text-ink" : "text-muted hover:text-ink"
                  )}
                >
                  {view.label}
                </button>
              ))}
            </div>
            {showArchived ? null : (
              <Button variant="primary" size="sm" onClick={() => setCreating((open) => !open)}>
                <Plus className="h-3.5 w-3.5" />
                New board
              </Button>
            )}
          </>
        }
      />

      {creating ? (
        <Card className="mb-4">
          <CardContent className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-[1fr_140px]">
              <div>
                <label className="text-xs font-medium text-muted" htmlFor="board-name">
                  Name
                </label>
                <Input
                  id="board-name"
                  className="mt-1"
                  value={name}
                  placeholder="Engineering"
                  onChange={(event) => {
                    setName(event.target.value);
                    if (!keyTouched) setKey(suggestKey(event.target.value));
                  }}
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted" htmlFor="board-key">
                  Key
                </label>
                <Input
                  id="board-key"
                  className="mt-1 font-mono uppercase"
                  value={key}
                  placeholder="ENG"
                  maxLength={10}
                  onChange={(event) => {
                    setKeyTouched(true);
                    setKey(event.target.value.toUpperCase());
                  }}
                />
              </div>
            </div>
            <p className="text-xs text-muted">
              Issues are named <span className="font-mono">{(key || "ENG").toUpperCase()}-1</span>,{" "}
              <span className="font-mono">{(key || "ENG").toUpperCase()}-2</span>. The key cannot be changed later.
            </p>
            <div>
              <label className="text-xs font-medium text-muted" htmlFor="board-project">
                Deploy project
              </label>
              <select
                id="board-project"
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
              <label className="text-xs font-medium text-muted" htmlFor="board-description">
                Description
              </label>
              <Textarea
                id="board-description"
                className="mt-1"
                value={description}
                placeholder="What this board tracks."
                onChange={(event) => setDescription(event.target.value)}
              />
            </div>
            {error ? <p className="text-sm text-danger">{error}</p> : null}
            <div className="flex justify-end gap-2">
              <Button variant="secondary" size="sm" onClick={() => setCreating(false)}>
                Cancel
              </Button>
              <Button
                variant="primary"
                size="sm"
                disabled={create.isPending || name.trim() === "" || key.trim().length < 2}
                onClick={() => create.mutate()}
              >
                {create.isPending ? "Creating…" : "Create board"}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {rows.length === 0 && !boards.isLoading ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-2 py-10 text-center">
            <KanbanSquare className="h-6 w-6 text-muted" aria-hidden />
            <p className="text-sm text-ink">{showArchived ? "No archived boards" : "No boards yet"}</p>
            <p className="max-w-sm text-xs text-muted">
              {showArchived
                ? "Archiving retires a finished board from this list without deleting its issues, so the identifiers they were given still resolve."
                : "A board holds issues and gives them identifiers like ENG-42, which a commit message can reference to close them on deploy."}
            </p>
          </CardContent>
        </Card>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {rows.map((board) => {
          const counts = ISSUE_STATUSES.map((status) => ({ status, count: board.issue_counts?.[status] ?? 0 }));
          const total = counts.reduce((sum, entry) => sum + entry.count, 0);
          const open = counts
            .filter(({ status }) => status !== "done" && status !== "canceled")
            .reduce((sum, entry) => sum + entry.count, 0);
          /* The per-status breakdown belongs on the board, not on the index --
           * five counts, mostly zero, buried the one number a card is read for.
           * It survives as the count's tooltip, which costs the card nothing. */
          const breakdown = counts
            .filter((entry) => entry.count > 0)
            .map(({ status, count }) => `${STATUS_LABELS[status]} ${count}`)
            .join(" · ");
          return (
            <Link key={board.id} to={`/boards/${board.id}`} className="block rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65">
              <Card className="h-full transition-colors hover:border-primary/40">
                <CardContent className="space-y-2">
                  <div className="flex items-baseline justify-between gap-2">
                    <div className="truncate text-sm font-medium text-ink">{board.name}</div>
                    <span className="shrink-0 text-xs text-muted" title={breakdown || undefined}>
                      {total === 0 ? "No issues" : `${open} open`}
                    </span>
                  </div>
                  <div className="truncate text-[11px] text-muted">
                    <span className="font-mono">{board.key}</span>
                    {board.project_name ? ` · Deploys via ${board.project_name}` : null}
                  </div>
                  {board.description ? <p className="line-clamp-2 text-xs text-muted">{board.description}</p> : null}
                </CardContent>
              </Card>
            </Link>
          );
        })}
      </div>
    </>
  );
}
