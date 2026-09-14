import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { Code2, Pilcrow } from "lucide-react";

import { Textarea } from "../ui/textarea";
import { IssueMarkdown } from "./IssueMarkdown";
import { cn } from "../../lib/utils";

/** The editor is a ProseMirror stack, and most pages never open a description:
 *  it is fetched when a description field appears rather than with the app. */
const IssueMarkdownEditor = lazy(() =>
  import("./IssueMarkdownEditor").then((module) => ({ default: module.IssueMarkdownEditor }))
);

/** Holds the editor's height while its chunk arrives, so opening a description
 *  does not shift the page under the pointer. */
function EditorLoading({ fill }: { fill: boolean }) {
  return (
    <div
      className={cn("mt-1 min-h-28 animate-pulse rounded-md bg-surface-2/40", fill && "min-h-0 flex-1")}
      aria-hidden
    />
  );
}

/** The description field: rendered markdown until you click into it, then an
 *  editor that renders what you type as you type it. A pasted or dropped
 *  screenshot is uploaded and placed at the caret.
 *
 *  The source view is one click away on purpose: markdown this editor has no
 *  schema for (a table, a nested list with unusual indentation) comes back
 *  normalized, and there has to be a way to see and fix that rather than
 *  discovering it after saving. */
export function IssueDescriptionField({
  value,
  onChange,
  placeholder,
  bare = false,
  grow = false,
  startEditing
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  /** Forces the field to open writable. An issue being filed has nothing to
   *  read yet, and expanding the composer mid-sentence must not drop the author
   *  into a read-only view of what they were still typing. */
  startEditing?: boolean;
  /** Drops the field's own border and background, for the composer where the
   *  title and description should read as one document rather than two inputs. */
  bare?: boolean;
  /** Fills the height it is given instead of sizing to its content, so the
   *  expanded composer hands the spare room to the description. */
  grow?: boolean;
}) {
  const [editing, setEditing] = useState(startEditing ?? value.trim() === "");
  const [source, setSource] = useState(false);
  // Whether the click that is about to blur the field landed inside it. A click
  // on a button of our own leaves a blur with no relatedTarget to inspect, and
  // it should not collapse the field.
  const pointerInside = useRef(false);

  // Clicking a description to edit it is the expected next move on this screen,
  // so the chunk is fetched while the description is still being read.
  useEffect(() => {
    if (editing) return;
    const timer = window.setTimeout(() => void import("./IssueMarkdownEditor"), 1200);
    return () => window.clearTimeout(timer);
  }, [editing]);

  return (
    <div
      className={cn(grow && "flex min-h-0 flex-1 flex-col")}
      onPointerDown={() => {
        pointerInside.current = true;
      }}
      onBlur={(event) => {
        // Leaving the field collapses it back to the read view; moving focus
        // between the editor, the source view and the buttons does not.
        const inside = pointerInside.current || event.currentTarget.contains(event.relatedTarget as Node | null);
        pointerInside.current = false;
        if (!inside && !startEditing && value.trim() !== "") setEditing(false);
      }}
    >
      {editing ? (
        source ? (
          <Textarea
            className={cn("mt-1 min-h-28 font-mono text-[13px]", bare && "rounded-none border-0 bg-transparent px-0", grow && "min-h-0 flex-1")}
            value={value}
            placeholder={placeholder}
            autoFocus={!bare}
            onChange={(event) => onChange(event.target.value)}
          />
        ) : (
          <div
            className={cn("mt-1 rounded-md", bare ? "px-0 py-1" : "border border-border bg-surface-2/20 px-3 py-2", grow && "flex min-h-0 flex-1 flex-col")}
          >
            <Suspense fallback={<EditorLoading fill={grow} />}>
              <IssueMarkdownEditor
                value={value}
                onChange={onChange}
                placeholder={placeholder}
                autoFocus={!bare && value.trim() !== ""}
                fill={grow}
              />
            </Suspense>
          </div>
        )
      ) : (
        <button
          type="button"
          className={cn(
            "mt-1 block w-full rounded-md border border-transparent text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65",
            bare ? "px-0 py-1" : "px-3 py-2 hover:border-border hover:bg-surface-2/40",
            grow && "scrollbar-subtle min-h-0 flex-1 overflow-y-auto"
          )}
          onClick={() => setEditing(true)}
          aria-label="Edit description"
        >
          <IssueMarkdown text={value} />
        </button>
      )}

      {editing ? (
        <div className="mt-1.5 flex shrink-0 flex-wrap items-center gap-2 text-[11px] text-muted">
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded px-1 py-0.5 hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65"
            onClick={() => setSource((current) => !current)}
            aria-label={source ? "Switch to the rich text editor" : "Edit the markdown source"}
          >
            {source ? <Pilcrow className="h-3 w-3" aria-hidden /> : <Code2 className="h-3 w-3" aria-hidden />}
            {source ? "Rich text" : "Markdown"}
          </button>
        </div>
      ) : null}
    </div>
  );
}
