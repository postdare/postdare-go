import { Suspense, lazy, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, Paperclip, Pencil, Trash2 } from "lucide-react";

import {
  createIssueComment,
  deleteIssueComment,
  listIssueComments,
  updateIssueComment
} from "../../api/postdareGo";
import type { IssueComment } from "../../api/types";
import type { MarkdownEditorHandle } from "./IssueMarkdownEditor";
import { IssueMarkdown } from "./IssueMarkdown";
import { cn, formatTimeAgo } from "../../lib/utils";
import { useAuthStore } from "../../store/auth";
import { toast } from "../../store/toast";

/** The same lazily-loaded editor the description uses: one chunk, fetched the
 *  first time either field needs it. */
const IssueMarkdownEditor = lazy(() =>
  import("./IssueMarkdownEditor").then((module) => ({ default: module.IssueMarkdownEditor }))
);

/** An issue's thread: what has been said, oldest first, and a box to say the
 *  next thing. Comments are markdown, written in the same editor as the
 *  description, so a screenshot can be pasted straight into a reply. */
export function IssueComments({ issueID }: { issueID: string }) {
  const token = useAuthStore((state) => state.token);
  const queryClient = useQueryClient();
  const comments = useQuery({
    queryKey: ["issue-comments", issueID],
    queryFn: () => listIssueComments(issueID, token)
  });

  function refresh() {
    queryClient.invalidateQueries({ queryKey: ["issue-comments", issueID] });
    // The card shows the thread's size, and the board is what renders the card.
    queryClient.invalidateQueries({ queryKey: ["board-issues"] });
  }

  const post = useMutation({
    mutationFn: (body: string) => createIssueComment(issueID, body, token),
    onSuccess: refresh,
    onError: (error: Error) => toast(error.message, "danger")
  });

  const thread = comments.data?.data ?? [];

  return (
    <section className="space-y-4" aria-label="Comments">
      <div className="flex items-baseline gap-2">
        <h2 className="text-xs font-medium text-muted">Comments</h2>
        {thread.length > 0 ? <span className="text-[11px] text-muted">{thread.length}</span> : null}
      </div>

      {comments.isError ? (
        <p className="text-sm text-muted">This thread could not be loaded.</p>
      ) : (
        <ol className="space-y-4">
          {thread.map((comment) => (
            <li key={comment.id}>
              <CommentRow comment={comment} onChanged={refresh} />
            </li>
          ))}
        </ol>
      )}

      <CommentComposer
        placeholder="Leave a comment…"
        submitLabel="Comment"
        pending={post.isPending}
        onSubmit={(body) => post.mutate(body)}
        resetOnSubmit
      />
    </section>
  );
}

function CommentRow({ comment, onChanged }: { comment: IssueComment; onChanged: () => void }) {
  const token = useAuthStore((state) => state.token);
  const user = useAuthStore((state) => state.user);
  const [editing, setEditing] = useState(false);

  // Who may change a comment is decided by the server; the buttons only avoid
  // offering an action that would come back 403.
  const mine = user != null && comment.author_id != null && user.id === comment.author_id;
  const canWrite = mine || user?.role === "admin";
  const author = comment.author_name ?? "Someone";

  const save = useMutation({
    mutationFn: (body: string) => updateIssueComment(comment.id, body, token),
    onSuccess: () => {
      setEditing(false);
      onChanged();
    },
    onError: (error: Error) => toast(error.message, "danger")
  });

  const remove = useMutation({
    mutationFn: () => deleteIssueComment(comment.id, token),
    onSuccess: onChanged,
    onError: (error: Error) => toast(error.message, "danger")
  });

  return (
    <article className="group flex gap-3">
      <span
        aria-hidden
        className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-surface-2 text-[10px] uppercase text-ink"
      >
        {author.slice(0, 1)}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-[11px] text-muted">
          <span className="font-medium text-ink">{author}</span>
          <time dateTime={comment.created_at ?? undefined}>{formatTimeAgo(comment.created_at)}</time>
          {comment.edited ? <span>edited</span> : null}
          {canWrite && !editing ? (
            // The controls stay out of the way until the comment is pointed at
            // or reached from the keyboard, so a thread reads as prose.
            <span className="ml-auto flex items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
              <RowAction label="Edit comment" onClick={() => setEditing(true)}>
                <Pencil className="h-3 w-3" aria-hidden />
              </RowAction>
              <RowAction
                label="Delete comment"
                onClick={() => remove.mutate()}
                disabled={remove.isPending}
                className="hover:text-danger"
              >
                <Trash2 className="h-3 w-3" aria-hidden />
              </RowAction>
            </span>
          ) : null}
        </div>

        {editing ? (
          <div className="mt-1.5">
            <CommentComposer
              initialValue={comment.body}
              placeholder="Leave a comment…"
              submitLabel="Save"
              pending={save.isPending}
              autoFocus
              onSubmit={(body) => save.mutate(body)}
              onCancel={() => setEditing(false)}
            />
          </div>
        ) : (
          <IssueMarkdown text={comment.body} className="mt-1" />
        )}
      </div>
    </article>
  );
}

function RowAction({
  label,
  onClick,
  disabled,
  className,
  children
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "rounded p-1 text-muted transition-colors hover:text-ink focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 disabled:opacity-45",
        className
      )}
    >
      {children}
    </button>
  );
}

/** The box the comment is written in: the markdown editor, a paperclip that
 *  uploads an image to the caret, and a send button. Cmd/Ctrl+Enter posts,
 *  because a comment is a message and that is how a message is sent. */
function CommentComposer({
  initialValue = "",
  placeholder,
  submitLabel,
  pending,
  autoFocus = false,
  resetOnSubmit = false,
  onSubmit,
  onCancel
}: {
  initialValue?: string;
  placeholder?: string;
  submitLabel: string;
  pending: boolean;
  autoFocus?: boolean;
  /** Empties the box once the comment is away, for the box that stays mounted
   *  at the foot of the thread. */
  resetOnSubmit?: boolean;
  onSubmit: (body: string) => void;
  onCancel?: () => void;
}) {
  const [value, setValue] = useState(initialValue);
  const [uploading, setUploading] = useState(false);
  const editor = useRef<MarkdownEditorHandle | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  // Read inside the editor's handlers, which are bound once at mount.
  const latest = useRef({ value, pending });
  latest.current = { value, pending };

  function submit() {
    const body = latest.current.value.trim();
    if (body === "" || latest.current.pending) return;
    onSubmit(body);
    if (resetOnSubmit) {
      // Cleared straight away rather than after the response: the mutation
      // reports a failure through a toast, and a box that empties only on
      // success leaves the author staring at a comment they already sent.
      editor.current?.clear();
      editor.current?.focus();
    }
  }

  /** The paperclip takes the same path a pasted screenshot does, so an image
   *  arrives the same way however it was chosen. */
  async function attach(files: FileList | null) {
    const images = Array.from(files ?? []).filter((file) => file.type.startsWith("image/"));
    if (images.length === 0) return;
    setUploading(true);
    try {
      await editor.current?.insertImages(images);
    } finally {
      setUploading(false);
    }
  }

  const empty = value.trim() === "";

  return (
    <div className="rounded-lg border border-border bg-surface-2/20 px-3 py-2 focus-within:border-primary/40">
      <Suspense fallback={<div className="min-h-14 animate-pulse rounded-md bg-surface-2/40" aria-hidden />}>
        <IssueMarkdownEditor
          value={initialValue}
          placeholder={placeholder}
          autoFocus={autoFocus}
          onChange={setValue}
          onSubmit={submit}
          onReady={(handle) => {
            editor.current = handle;
          }}
        />
      </Suspense>

      <div className="mt-1.5 flex items-center justify-end gap-1">
        {onCancel ? (
          <button
            type="button"
            className="mr-auto rounded px-2 py-1 text-[11px] text-muted transition-colors hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65"
            onClick={onCancel}
          >
            Cancel
          </button>
        ) : null}
        <input
          ref={fileInput}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp"
          multiple
          className="hidden"
          onChange={(event) => {
            void attach(event.target.files);
            // Cleared so picking the same file twice still fires a change.
            event.target.value = "";
          }}
        />
        <button
          type="button"
          title="Attach an image"
          aria-label="Attach an image"
          disabled={uploading}
          onClick={() => fileInput.current?.click()}
          className="rounded p-1.5 text-muted transition-colors hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 disabled:opacity-45"
        >
          <Paperclip className={cn("h-4 w-4", uploading && "animate-pulse")} aria-hidden />
        </button>
        <button
          type="button"
          title={`${submitLabel} (⌘↵)`}
          aria-label={submitLabel}
          disabled={empty || pending}
          onClick={submit}
          className={cn(
            "inline-flex h-7 w-7 items-center justify-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65",
            empty || pending ? "bg-surface-2 text-muted" : "bg-primary text-primary-ink hover:bg-primary/90"
          )}
        >
          <ArrowUp className="h-4 w-4" aria-hidden />
        </button>
      </div>
    </div>
  );
}
