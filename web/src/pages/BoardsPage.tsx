import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KanbanSquare, Plus } from "lucide-react";

import { createBoard, listBoards, listProjects } from "../api/postdareGo";
import { PageHeader } from "../components/PageHeader";
import { ISSUE_STATUSES, STATUS_DOT, STATUS_LABELS } from "../components/board/boardMeta";
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

  const boards = useQuery({ queryKey: ["boards"], queryFn: () => listBoards(token) });
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
        description="Issues grouped into boards. A commit that says fix ENG-42 closes the issue when its deploy succeeds."
        actions={
          <Button variant="primary" size="sm" onClick={() => setCreating((open) => !open)}>
            <Plus className="h-3.5 w-3.5" />
            New board
          </Button>
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
            <p className="text-sm text-ink">No boards yet</p>
            <p className="max-w-sm text-xs text-muted">
              A board holds issues and gives them identifiers like ENG-42, which a commit message can reference to close
              them on deploy.
            </p>
          </CardContent>
        </Card>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {rows.map((board) => {
          const open = ISSUE_STATUSES.filter((status) => status !== "done" && status !== "canceled").reduce(
            (total, status) => total + (board.issue_counts?.[status] ?? 0),
            0
          );
          return (
            <Link key={board.id} to={`/boards/${board.id}`} className="block rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65">
              <Card className="h-full transition-colors hover:border-primary/40">
                <CardContent className="space-y-3">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium text-ink">{board.name}</div>
                      <div className="mt-0.5 font-mono text-[11px] text-muted">{board.key}</div>
                    </div>
                    <span className="shrink-0 text-xs text-muted">{open} open</span>
                  </div>
                  {board.description ? <p className="line-clamp-2 text-xs text-muted">{board.description}</p> : null}
                  <div className="flex flex-wrap gap-x-3 gap-y-1">
                    {ISSUE_STATUSES.map((status) => (
                      <span key={status} className="inline-flex items-center gap-1 text-[11px] text-muted">
                        <span aria-hidden className={cn("h-1.5 w-1.5 rounded-full", STATUS_DOT[status])} />
                        {STATUS_LABELS[status]} {board.issue_counts?.[status] ?? 0}
                      </span>
                    ))}
                  </div>
                  {board.project_name ? (
                    <p className="text-[11px] text-muted">Deploys via {board.project_name}</p>
                  ) : null}
                </CardContent>
              </Card>
            </Link>
          );
        })}
      </div>
    </>
  );
}
