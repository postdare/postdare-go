import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent
} from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { ArrowLeft, Plus } from "lucide-react";

import { getBoard, listBoardIssues, moveIssue } from "../api/postdareGo";
import { streamURL } from "../api/client";
import type { Issue, IssueStatus } from "../api/types";
import { BoardMenu } from "../components/board/BoardMenu";
import { IssueComposer } from "../components/board/IssueComposer";
import { IssueDragPreview, SortableIssueCard } from "../components/board/IssueCard";
import { ISSUE_STATUSES, STATUS_DOT, STATUS_LABELS, isIssueStatus } from "../components/board/boardMeta";
import { PageHeader } from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { cn } from "../lib/utils";
import { useAuthStore } from "../store/auth";

type Columns = Record<IssueStatus, Issue[]>;

function emptyColumns(): Columns {
  return { backlog: [], todo: [], in_progress: [], done: [], canceled: [] };
}

function groupIssues(issues: Issue[]): Columns {
  const columns = emptyColumns();
  for (const issue of issues) {
    if (isIssueStatus(issue.status)) columns[issue.status].push(issue);
  }
  return columns;
}

function Column({
  status,
  issues,
  activeID,
  onOpen,
  onAdd
}: {
  status: IssueStatus;
  issues: Issue[];
  activeID: number | null;
  onOpen: (issue: Issue) => void;
  onAdd: (status: IssueStatus) => void;
}) {
  // The column itself is a drop target so a card can be dropped into an empty
  // one, where there is no sortable item to aim at.
  const { setNodeRef, isOver } = useDroppable({ id: `column:${status}`, data: { status } });

  return (
    // A column keeps its width at every viewport instead of sharing the space
    // with the others. Squeezing five columns into the window makes each one too
    // narrow to read a title in, and a board is meant to be scrolled sideways.
    <section className="flex h-full w-[336px] shrink-0 flex-col">
      <header className="flex h-9 shrink-0 items-center justify-between gap-2 px-1">
        <div className="flex min-w-0 items-center gap-2">
          <span aria-hidden className={cn("h-2 w-2 shrink-0 rounded-full", STATUS_DOT[status])} />
          <h2 className="truncate text-sm font-medium text-ink">{STATUS_LABELS[status]}</h2>
          <span className="text-xs text-muted">{issues.length}</span>
        </div>
        <Button variant="ghost" size="sm" className="h-7 px-1.5" onClick={() => onAdd(status)} aria-label={`Add issue to ${STATUS_LABELS[status]}`}>
          <Plus className="h-3.5 w-3.5" />
        </Button>
      </header>
      <div
        ref={setNodeRef}
        className={cn(
          // Each column scrolls by itself: a long Backlog must not push Done off
          // the bottom of the page, and the column headers stay put while it does.
          "scrollbar-subtle mt-1 flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto rounded-lg border border-dashed border-transparent p-1 transition-colors",
          isOver && "border-primary/40 bg-primary/5"
        )}
      >
        <SortableContext items={issues.map((issue) => issue.id)} strategy={verticalListSortingStrategy}>
          {issues.map((issue) => (
            <SortableIssueCard key={issue.id} issue={issue} onOpen={onOpen} />
          ))}
        </SortableContext>
        {issues.length === 0 && activeID === null ? (
          <p className="px-2 py-6 text-center text-xs text-muted">Nothing here</p>
        ) : null}
      </div>
    </section>
  );
}

export function BoardPage() {
  const { id = "" } = useParams();
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const [search, setSearch] = useState("");
  const [activeID, setActiveID] = useState<number | null>(null);
  const [composerOpen, setComposerOpen] = useState(false);
  const [composerStatus, setComposerStatus] = useState<IssueStatus>("backlog");

  const issuesKey = useMemo(() => ["board-issues", id] as const, [id]);
  const board = useQuery({ queryKey: ["board", id], queryFn: () => getBoard(id, token), enabled: Boolean(id) });
  const issues = useQuery({ queryKey: issuesKey, queryFn: () => listBoardIssues(id, token), enabled: Boolean(id) });

  // A second person's drag, or a deploy closing an issue, arrives on the board's
  // event stream. The message only names the change, so the board refetches
  // rather than merging a partial update into a card someone may be dragging.
  const refetchTimer = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!id || !token) return;
    const source = new EventSource(streamURL(`/api/v1/boards/${id}/stream`, token));
    source.onmessage = () => {
      window.clearTimeout(refetchTimer.current);
      refetchTimer.current = window.setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: ["board-issues", id] });
        queryClient.invalidateQueries({ queryKey: ["board", id] });
      }, 250);
    };
    source.onerror = () => source.close();
    return () => {
      window.clearTimeout(refetchTimer.current);
      source.close();
    };
  }, [id, token, queryClient]);

  const allIssues = issues.data?.data ?? [];
  const visibleIssues = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return allIssues;
    return allIssues.filter(
      (issue) =>
        issue.title.toLowerCase().includes(term) ||
        issue.identifier.toLowerCase().includes(term) ||
        (issue.assignee_name ?? "").toLowerCase().includes(term) ||
        issue.labels.some((label) => label.toLowerCase().includes(term))
    );
  }, [allIssues, search]);
  const columns = useMemo(() => groupIssues(visibleIssues), [visibleIssues]);
  const activeIssue = activeID === null ? null : allIssues.find((issue) => issue.id === activeID) ?? null;

  const sensors = useSensors(
    // A small activation distance keeps a click on a card from starting a drag,
    // so the card can be both draggable and the way you open an issue.
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const move = useMutation({
    mutationFn: (input: { issueID: number; status: IssueStatus; after_id?: number; before_id?: number }) =>
      moveIssue(input.issueID, { status: input.status, after_id: input.after_id, before_id: input.before_id }, token),
    onSettled: () => queryClient.invalidateQueries({ queryKey: issuesKey })
  });

  // An existing issue is a page: it has a URL worth sending to someone, and a
  // description full of screenshots needs the room.
  function openIssue(issue: Issue) {
    navigate(`/issues/${issue.id}`);
  }

  // Filing one is a modal, because most issues are a title and a status and
  // leaving the board to type them is the wrong trade. The composer expands in
  // place when one of them turns out to need the room.
  function addIssue(status: IssueStatus) {
    setComposerStatus(status);
    setComposerOpen(true);
  }

  function handleDragStart(event: DragStartEvent) {
    setActiveID(Number(event.active.id));
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    setActiveID(null);
    if (!over) return;

    const issueID = Number(active.id);
    const dragged = allIssues.find((issue) => issue.id === issueID);
    if (!dragged) return;

    const overID = String(over.id);
    const targetStatus = overID.startsWith("column:")
      ? (overID.slice("column:".length) as IssueStatus)
      : ((over.data.current?.status as IssueStatus) ?? dragged.status);
    if (!isIssueStatus(targetStatus)) return;

    // Resolve the drop to the two cards it landed between. Neighbours travel to
    // the server instead of an index, so a board that changed while the card was
    // in the air still puts it where the user aimed.
    const column = (columns[targetStatus] ?? []).filter((issue) => issue.id !== issueID);
    let insertAt = column.length;
    if (!overID.startsWith("column:")) {
      const overIndex = column.findIndex((issue) => issue.id === Number(over.id));
      if (overIndex >= 0) {
        const fromSameColumn = dragged.status === targetStatus;
        const originalIndex = (columns[targetStatus] ?? []).findIndex((issue) => issue.id === issueID);
        insertAt = fromSameColumn && originalIndex < overIndex ? overIndex + 1 : overIndex;
      }
    }
    const after = column[insertAt - 1];
    const before = column[insertAt];
    if (dragged.status === targetStatus && after?.id === undefined && before?.id === dragged.id) return;

    const next = [...column];
    next.splice(insertAt, 0, { ...dragged, status: targetStatus });
    const reordered = allIssues
      .filter((issue) => issue.id !== issueID && issue.status !== targetStatus)
      .concat(next.map((issue) => (issue.id === issueID ? { ...issue, status: targetStatus } : issue)));

    queryClient.setQueryData(issuesKey, { ...issues.data, data: reordered });
    move.mutate({ issueID, status: targetStatus, after_id: after?.id, before_id: before?.id });
  }

  const boardData = board.data?.data;
  const boardKey = boardData?.key ?? "";

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title={boardData?.name ?? "Board"}
        description={boardData?.description || (boardData ? `${boardKey} · ${allIssues.length} issues` : undefined)}
        actions={
          <>
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Filter issues"
              aria-label="Filter issues"
              className="h-9 w-44"
            />
            <Button variant="primary" size="sm" onClick={() => addIssue("backlog")}>
              <Plus className="h-3.5 w-3.5" />
              New issue
            </Button>
            {boardData ? <BoardMenu board={boardData} /> : null}
          </>
        }
      />

      {boardData?.archived_at ? (
        <p className="mb-3 shrink-0 rounded-md border border-border bg-surface-2 px-3 py-2 text-xs text-muted">
          This board is archived. It still works, and its issues keep their identifiers, but it no longer appears on
          the boards index. Restore it from the board menu.
        </p>
      ) : null}

      <Link to="/boards" className="mb-3 inline-flex shrink-0 items-center gap-1 text-xs text-muted hover:text-ink">
        <ArrowLeft className="h-3 w-3" />
        All boards
      </Link>

      {issues.isError ? (
        <p className="shrink-0 rounded-md border border-danger/35 bg-danger/10 p-3 text-sm text-danger">
          Could not load this board's issues.
        </p>
      ) : null}

      <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
        <div className="scrollbar-subtle -mx-4 flex min-h-0 flex-1 gap-3 overflow-x-auto px-4 pb-2 md:mx-0 md:px-0">
          {ISSUE_STATUSES.map((status) => (
            <Column
              key={status}
              status={status}
              issues={columns[status]}
              activeID={activeID}
              onOpen={openIssue}
              onAdd={addIssue}
            />
          ))}
        </div>
        <DragOverlay>{activeIssue ? <IssueDragPreview issue={activeIssue} /> : null}</DragOverlay>
      </DndContext>

      <IssueComposer
        // Remounted per opening so each new issue starts from a clean draft
        // rather than whatever the last one left behind.
        key={composerOpen ? `${composerStatus}-open` : "closed"}
        open={composerOpen}
        boardID={id}
        boardKey={boardKey}
        defaultStatus={composerStatus}
        onClose={() => setComposerOpen(false)}
        onCreated={() => queryClient.invalidateQueries({ queryKey: issuesKey })}
      />

    </div>
  );
}
