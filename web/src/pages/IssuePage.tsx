import { useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Copy, Save, Trash2 } from "lucide-react";

import { createIssue, deleteIssue, getBoard, getIssue, listBoardLabels, listBoardUsers, updateIssue } from "../api/postdareGo";
import type { Issue, IssuePriority, IssueStatus } from "../api/types";
import { IssueDescriptionField } from "../components/board/IssueDescription";
import { isIssueStatus } from "../components/board/boardMeta";
import { AssigneePicker, PriorityPicker, StatusPicker } from "../components/board/IssueProperties";
import { LabelsEditor } from "../components/board/LabelsEditor";
import { PageHeader } from "../components/PageHeader";
import { Badge, statusTone } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { formatDate, shortCommit } from "../lib/utils";
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
  const [copied, setCopied] = useState(false);

  const save = useMutation({
    mutationFn: () => {
      const payload = {
        title: draft.title.trim(),
        description: draft.description,
        status: draft.status,
        priority: draft.priority,
        labels,
        ...(draft.assignee_id === null ? { clear_assignee: true } : { assignee_id: draft.assignee_id })
      };
      return isNew ? createIssue(boardID, payload, token) : updateIssue(issueId, payload, token);
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      queryClient.invalidateQueries({ queryKey: ["board-labels"] });
      if (isNew) {
        navigate(`/issues/${res.data.id}`, { replace: true });
        return;
      }
      queryClient.invalidateQueries({ queryKey: ["issue", issueId] });
    }
  });

  const remove = useMutation({
    mutationFn: () => deleteIssue(issueId, token),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["board-issues"] });
      queryClient.invalidateQueries({ queryKey: ["boards"] });
      navigate(`/boards/${loaded?.board_id ?? boardID}`);
    }
  });

  const backTo = `/boards/${loaded?.board_id ?? boardID}`;
  const identifier = loaded?.identifier ?? `${boardKey ?? ""}-…`;

  async function copyReference() {
    if (!loaded) return;
    try {
      await navigator.clipboard.writeText(`fix ${loaded.identifier}`);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

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
            <Button variant="primary" size="sm" onClick={() => save.mutate()} disabled={save.isPending || draft.title.trim() === ""}>
              <Save className="h-3.5 w-3.5" />
              {save.isPending ? "Saving…" : isNew ? "Create issue" : "Save"}
            </Button>
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
                onChange={(event) => setDraft({ ...draft, title: event.target.value })}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) save.mutate();
                }}
              />
            </div>
            <div>
              <span className={fieldLabel}>Description</span>
              <IssueDescriptionField
                value={draft.description}
                placeholder="Context, reproduction, links. Paste a screenshot straight in."
                onChange={(description) => setDraft({ ...draft, description })}
                startEditing={isNew}
              />
            </div>
            {save.error ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">{save.error.message}</p>
            ) : null}
            {remove.error ? (
              <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">{remove.error.message}</p>
            ) : null}
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card>
            <CardContent className="space-y-1">
              <span className={fieldLabel}>Properties</span>
              <div className="space-y-0.5 pt-0.5">
                <StatusPicker layout="row" value={draft.status} onChange={(status) => setDraft({ ...draft, status })} />
                <PriorityPicker layout="row" value={draft.priority} onChange={(priority) => setDraft({ ...draft, priority })} />
                <AssigneePicker
                  layout="row"
                  value={draft.assignee_id}
                  users={users.data?.data ?? []}
                  onChange={(assignee_id) => setDraft({ ...draft, assignee_id })}
                />
              </div>

              <div className="pt-3">
                <span className={fieldLabel}>Labels</span>
                <div className="pt-1.5">
                  <LabelsEditor value={labels} suggestions={boardLabels.data?.data ?? []} onChange={setLabels} />
                </div>
              </div>

              {loaded?.completed_at ? (
                <p className="pt-3 text-[11px] text-muted">Completed {formatDate(loaded.completed_at)}</p>
              ) : null}
            </CardContent>
          </Card>

          {!isNew && loaded ? (
            <Card>
              <CardContent className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium text-ink">Releases</span>
                  <Button variant="ghost" size="sm" className="h-7" onClick={copyReference}>
                    <Copy className="h-3 w-3" />
                    {copied ? "Copied" : `fix ${loaded.identifier}`}
                  </Button>
                </div>
                <p className="text-[11px] text-muted">
                  Put <span className="font-mono">fix {loaded.identifier}</span> in a commit message and the deploy it lands
                  in closes this issue.
                </p>
                {loaded.deploy_links.length > 0 ? (
                  <ul className="space-y-1.5 pt-1">
                    {loaded.deploy_links.map((link) => (
                      <li key={link.task_id} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
                        <Link to={`/deploy-tasks/${link.task_id}`} className="font-medium text-primary hover:underline">
                          #{link.task_id}
                        </Link>
                        <Badge tone={statusTone(link.status)}>{link.status}</Badge>
                        <span className="text-muted">{link.project_name ?? `Project ${link.project_id}`}</span>
                        {link.commit_id ? <span className="font-mono text-muted">{shortCommit(link.commit_id)}</span> : null}
                        {link.closing ? <span className="text-muted">· closes</span> : null}
                        {link.source === "manual" ? <span className="text-muted">· linked by hand</span> : null}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-xs text-muted">No release has carried this issue yet.</p>
                )}
              </CardContent>
            </Card>
          ) : null}
        </div>
      </div>
    </>
  );
}
