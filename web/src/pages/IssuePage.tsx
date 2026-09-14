import { useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, Check, Loader2, Save, Trash2 } from "lucide-react";

import { createIssue, deleteIssue, getBoard, getIssue, listBoardLabels, listBoardUsers } from "../api/postdareGo";
import type { Issue, IssuePriority, IssueStatus } from "../api/types";
import { IssueComments } from "../components/board/IssueComments";
import { IssueDescriptionField } from "../components/board/IssueDescription";
import { isIssueStatus } from "../components/board/boardMeta";
import { AssigneePicker, PriorityPicker, StatusPicker } from "../components/board/IssueProperties";
import { LabelsEditor } from "../components/board/LabelsEditor";
import { PageHeader } from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { useIssueAutosave, type SaveState } from "../hooks/useIssueAutosave";
import { formatDate } from "../lib/utils";
import { useAuthStore } from "../store/auth";

interface IssueDraft {
  title: string;
  description: string;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_id: number | null;
  labels: string[];
}

function draftFrom(issue: Issue | undefined, fallbackStatus: IssueStatus): IssueDraft {
  return {
    title: issue?.title ?? "",
    description: issue?.description ?? "",
    status: issue?.status ?? fallbackStatus,
    priority: issue?.priority ?? "none",
    assignee_id: issue?.assignee_id ?? null,
    labels: issue?.labels ?? []
  };
}

const fieldLabel = "text-xs font-medium text-muted";

/** What replaced the save button: a line that says where the edit got to.
 *
 *  It stays quiet while nothing is happening. An issue being edited is saved
 *  continuously, so a control that has to be pressed would only ever be a way
 *  to lose work by not pressing it. */
function SaveIndicator({ state, error }: { state: SaveState; error: string | null }) {
  if (state === "idle") return null;
  if (state === "error") {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-danger" role="status">
        <AlertCircle className="h-3.5 w-3.5" aria-hidden />
        {error ?? "Not saved"}
      </span>
    );
  }
  const saving = state === "saving";
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted" role="status" aria-live="polite">
      {saving ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
      ) : state === "saved" ? (
        <Check className="h-3.5 w-3.5" aria-hidden />
      ) : null}
      {saving ? "Saving…" : state === "saved" ? "Saved" : "Unsaved"}
    </span>
  );
}


export function IssuePage() {
  const { boardId, issueId } = useParams();
  const [searchParams] = useSearchParams();
  const token = useAuthStore((state) => state.token);
  const isNew = !issueId;

  const requestedStatus = searchParams.get("status") ?? "";
  const fallbackStatus: IssueStatus = isIssueStatus(requestedStatus) ? requestedStatus : "backlog";

  const issue = useQuery({ queryKey: ["issue", issueId], queryFn: () => getIssue(issueId!, token), enabled: !isNew });
  const loaded = issue.data?.data;
  const boardKeyOwner = loaded?.board_id ?? (boardId ? Number(boardId) : undefined);
  const board = useQuery({
    queryKey: ["board", String(boardKeyOwner ?? "")],
    queryFn: () => getBoard(boardKeyOwner!, token),
    enabled: Boolean(boardKeyOwner)
  });

  // The form is not mounted until the issue is in hand, and it seeds its state
  // from that issue on mount rather than filling it in from an effect. Both
  // halves matter: a form mounted against a blank issue latches anything it
  // derives on first render -- such as whether the description starts rendered
  // or in edit mode -- onto the blank one, and no later effect undoes that.
  if (!isNew && !loaded) {
    return (
      <>
        <PageHeader title="Issue" />
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted">
            {issue.isError ? "This issue could not be loaded." : "Loading…"}
          </CardContent>
        </Card>
      </>
    );
  }

  return (
    <IssueForm
      key={loaded?.id ?? "new"}
      issue={loaded}
      boardID={boardId ?? String(loaded?.board_id ?? "")}
      boardName={board.data?.data?.name}
      boardKey={board.data?.data?.key}
      fallbackStatus={fallbackStatus}
    />
  );
}

function IssueForm({
  issue: loaded,
  boardID,
  boardName,
  boardKey,
  fallbackStatus
}: {
  issue: Issue | undefined;
  boardID: string;
  boardName?: string;
  boardKey?: string;
  fallbackStatus: IssueStatus;
}) {
  const navigate = useNavigate();
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const isNew = !loaded;
  const issueId = loaded ? String(loaded.id) : "";

  const users = useQuery({ queryKey: ["board-users"], queryFn: () => listBoardUsers(token) });
  const boardLabels = useQuery({
    queryKey: ["board-labels", boardID],
    queryFn: () => listBoardLabels(boardID, token),
    enabled: Boolean(boardID)
  });

  const [draft, setDraft] = useState<IssueDraft>(() => draftFrom(loaded, fallbackStatus));
  const [labels, setLabels] = useState<string[]>(() => loaded?.labels ?? []);

  // An issue that exists saves itself; one that does not yet exist has nothing
  // to save to, so filing it stays a deliberate press.
  const autosave = useIssueAutosave(issueId, {
    enabled: !isNew,
    onSaved: () => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      queryClient.invalidateQueries({ queryKey: ["board-labels"] });
    }
  });

  /** Applies an edit to the form and queues the same change for the server.
   *  The two move together so the field the author is looking at and the field
   *  that gets written can never be two different things. */
  function edit(changes: Partial<IssueDraft>, payload: Record<string, unknown>, immediate = false) {
    setDraft((current) => ({ ...current, ...changes }));
    autosave.queue(payload, { immediate });
  }

  const create = useMutation({
    mutationFn: () =>
      createIssue(
        boardID,
        {
          title: draft.title.trim(),
          description: draft.description,
          status: draft.status,
          priority: draft.priority,
          labels,
          ...(draft.assignee_id === null ? { clear_assignee: true } : { assignee_id: draft.assignee_id })
        },
        token
      ),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      queryClient.invalidateQueries({ queryKey: ["board-labels"] });
      navigate(`/issues/${res.data.id}`, { replace: true });
    }
  });

  const remove = useMutation({
    // The edits stop mattering the moment the issue does, and an unmount that
    // flushed them would PATCH a row that is already gone.
    onMutate: () => autosave.cancel(),
    mutationFn: () => deleteIssue(issueId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      navigate(`/boards/${loaded?.board_id ?? boardID}`);
    }
  });

  const backTo = `/boards/${loaded?.board_id ?? boardID}`;
  const identifier = loaded?.identifier ?? `${boardKey ?? ""}-…`;

  return (
    <>
      <PageHeader
        title={isNew ? "New issue" : identifier}
        description={
          isNew
            ? boardName
              ? `Files as the next issue on ${boardName} (${boardKey}).`
              : undefined
            : `Opened ${formatDate(loaded?.created_at)}${loaded?.creator_name ? ` by ${loaded.creator_name}` : ""}`
        }
        actions={
          <>
            {!isNew ? (
              <Button
                variant="ghost"
                size="sm"
                className="text-danger hover:text-danger"
                onClick={() => remove.mutate()}
                disabled={remove.isPending}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Delete
              </Button>
            ) : null}
            {isNew ? (
              <Button
                variant="primary"
                size="sm"
                onClick={() => create.mutate()}
                disabled={create.isPending || draft.title.trim() === ""}
              >
                <Save className="h-3.5 w-3.5" />
                {create.isPending ? "Filing…" : "Create issue"}
              </Button>
            ) : (
              <SaveIndicator state={autosave.state} error={autosave.error} />
            )}
          </>
        }
      />

      <Link to={backTo} className="mb-4 inline-flex items-center gap-1 text-xs text-muted hover:text-ink">
        <ArrowLeft className="h-3 w-3" />
        {boardName ?? "Board"}
      </Link>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
        <Card>
          <CardContent className="space-y-4">
            <div>
              <label className={fieldLabel} htmlFor="issue-title">
                Title
              </label>
              <Input
                id="issue-title"
                className="mt-1 h-10 text-base"
                value={draft.title}
                placeholder="What needs to happen?"
                autoFocus={isNew}
                onChange={(event) => {
                  const title = event.target.value;
                  // An empty title is refused by the server, and saving it as
                  // the author clears the box to retype would be a write that
                  // only ever fails. The box empties; the issue keeps its name
                  // until there is a new one.
                  if (title.trim() === "") {
                    setDraft((current) => ({ ...current, title }));
                    return;
                  }
                  edit({ title }, { title: title.trim() });
                }}
                onKeyDown={(event) => {
                  if (event.key !== "Enter") return;
                  event.preventDefault();
                  if (isNew) create.mutate();
                  else autosave.flush();
                }}
              />
              {!isNew && draft.title.trim() === "" ? (
                <p className="mt-1 text-[11px] text-muted">An issue keeps its title until you give it a new one.</p>
              ) : null}
            </div>
            <div>
              <span className={fieldLabel}>Description</span>
              <IssueDescriptionField
                value={draft.description}
                placeholder="Context, reproduction, links. Paste a screenshot straight in."
                onChange={(description) => edit({ description }, { description })}
                startEditing={isNew}
              />
            </div>
            {create.error ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">{create.error.message}</p>
            ) : null}
            {remove.error ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">{remove.error.message}</p>
            ) : null}

            {/* The thread lives under the description, inside the same card:
                it is the rest of what is known about this issue, not a separate
                panel. An unsaved issue has nothing to comment on yet. */}
            {!isNew ? (
              <div className="border-t border-border pt-4">
                <IssueComments issueID={issueId} />
              </div>
            ) : null}
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card>
            <CardContent className="space-y-1">
              <span className={fieldLabel}>Properties</span>
              <div className="space-y-0.5 pt-0.5">
                <StatusPicker layout="row" value={draft.status} onChange={(status) => edit({ status }, { status }, true)} />
                <PriorityPicker
                  layout="row"
                  value={draft.priority}
                  onChange={(priority) => edit({ priority }, { priority }, true)}
                />
                <AssigneePicker
                  layout="row"
                  value={draft.assignee_id}
                  users={users.data?.data ?? []}
                  onChange={(assignee_id) =>
                    edit(
                      { assignee_id },
                      assignee_id === null ? { clear_assignee: true } : { assignee_id },
                      true
                    )
                  }
                />
              </div>

              <div className="pt-3">
                <span className={fieldLabel}>Labels</span>
                <div className="pt-1.5">
                  <LabelsEditor
                    value={labels}
                    suggestions={boardLabels.data?.data ?? []}
                    onChange={(next) => {
                      setLabels(next);
                      autosave.queue({ labels: next }, { immediate: true });
                    }}
                  />
                </div>
              </div>

              {loaded?.completed_at ? (
                <p className="pt-3 text-[11px] text-muted">Completed {formatDate(loaded.completed_at)}</p>
              ) : null}
            </CardContent>
          </Card>

        </div>
      </div>
    </>
  );
}
