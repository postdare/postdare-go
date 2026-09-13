import { useEffect, useRef, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";
import { ImagePlus, Loader2 } from "lucide-react";

import { fetchAttachmentBlob, uploadAttachment } from "../../api/postdareGo";
import { Textarea } from "../ui/textarea";
import { useAuthStore } from "../../store/auth";
import { cn } from "../../lib/utils";

/** Matches the URL the upload endpoint hands back, so only this server's own
 *  attachments take the authenticated path; anything else stays a plain image. */
const ATTACHMENT_SRC = /^\/api\/v1\/attachments\/\d+$/;

/** Object URLs, held for the session instead of for one mount.
 *
 *  The field swaps between rendered markdown and a textarea, so a per-mount
 *  object URL meant every click into the description threw its images away and
 *  downloaded them back: the spinner, the layout jump and the image again, on
 *  every entry. The bytes never change once stored, so one object URL per
 *  attachment is both correct and what stops the description from blinking.
 *  The cap keeps a long session from pinning every screenshot it ever showed. */
const MAX_CACHED_ATTACHMENTS = 40;
const attachmentURLs = new Map<string, string>();
const attachmentRequests = new Map<string, Promise<string>>();

function loadAttachmentURL(src: string, token?: string | null) {
  const cached = attachmentURLs.get(src);
  if (cached) return Promise.resolve(cached);
  // A description with the same image twice, or a preview rendered beside the
  // raw text, must not fetch the same bytes twice.
  const inFlight = attachmentRequests.get(src);
  if (inFlight) return inFlight;
  const request = fetchAttachmentBlob(src, token).then((blob) => {
    const url = URL.createObjectURL(blob);
    attachmentURLs.set(src, url);
    // A Map iterates in insertion order, so the first key is the oldest.
    while (attachmentURLs.size > MAX_CACHED_ATTACHMENTS) {
      const oldest = attachmentURLs.keys().next().value as string;
      const evicted = attachmentURLs.get(oldest);
      attachmentURLs.delete(oldest);
      if (evicted) URL.revokeObjectURL(evicted);
    }
    return url;
  });
  attachmentRequests.set(src, request);
  void request.catch(() => undefined).finally(() => attachmentRequests.delete(src));
  return request;
}

/** An <img> for an attachment. The tag cannot send an Authorization header, so
 *  the bytes are fetched with one and handed over as an object URL. */
function AttachmentImage({ src, alt }: { src: string; alt?: string }) {
  const token = useAuthStore((state) => state.token);
  // Seeded from the cache so a remount paints the image instead of the spinner.
  const [objectURL, setObjectURL] = useState<string | undefined>(() => attachmentURLs.get(src));
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const cached = attachmentURLs.get(src);
    if (cached) {
      setObjectURL(cached);
      return;
    }
    setFailed(false);
    loadAttachmentURL(src, token)
      .then((url) => {
        if (!cancelled) setObjectURL(url);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [src, token]);

  if (failed) {
    return <span className="text-xs text-danger">[{alt || "image"} could not be loaded]</span>;
  }
  if (!objectURL) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-muted">
        <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
        Loading {alt || "image"}…
      </span>
    );
  }
  return <img src={objectURL} alt={alt ?? ""} className="max-h-80 rounded-md border border-border" loading="lazy" />;
}

/** Module scope on purpose. This object is the element type react-markdown
 *  renders for each tag, so a fresh literal per render is a new component type,
 *  which remounts every image it holds -- and a remount is another fetch and
 *  another flash. The live preview re-renders on every keystroke, so getting
 *  this wrong would download the images once per key. */
const markdownComponents: Components = {
  img: ({ src, alt }) =>
    typeof src === "string" && ATTACHMENT_SRC.test(src) ? (
      <AttachmentImage src={src} alt={alt} />
    ) : (
      <img src={typeof src === "string" ? src : undefined} alt={alt ?? ""} className="max-h-80 rounded-md border border-border" />
    ),
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {children}
    </a>
  )
};

export function IssueMarkdown({ text }: { text: string }) {
  return (
    <div className="space-y-2 text-sm leading-relaxed text-ink [&_a]:text-primary [&_a]:underline [&_code]:rounded [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[13px] [&_li]:ml-4 [&_li]:list-disc [&_p]:break-words [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-surface-2 [&_pre]:p-2">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

/** The description field: rendered markdown until you click into it, then a
 *  textarea with the rendered result live underneath it -- writing markdown
 *  against its own output is the only way to place an image or a table without
 *  saving to check. Pasting or dropping an image uploads it and writes the
 *  markdown at the cursor, which is the whole point of the field carrying
 *  images. */
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
  const token = useAuthStore((state) => state.token);
  const [editing, setEditing] = useState(startEditing ?? value.trim() === "");
  const [uploading, setUploading] = useState(0);
  const [error, setError] = useState<string>();
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  // Whether the click that is about to blur the textarea landed inside this
  // field. The preview is pointer-interactive, and a click on plain rendered
  // text moves focus nowhere at all, so relatedTarget alone cannot tell
  // "clicked my own preview" from "clicked the page".
  const pointerInside = useRef(false);

  function insertAtCursor(snippet: string) {
    const area = areaRef.current;
    if (!area) {
      onChange(value ? `${value}\n\n${snippet}` : snippet);
      return;
    }
    const start = area.selectionStart ?? value.length;
    const end = area.selectionEnd ?? value.length;
    const next = `${value.slice(0, start)}${snippet}${value.slice(end)}`;
    onChange(next);
    // Put the caret after what was inserted so typing continues where the
    // author was, rather than jumping to the end of the field.
    requestAnimationFrame(() => {
      area.focus();
      const caret = start + snippet.length;
      area.setSelectionRange(caret, caret);
    });
  }

  async function upload(files: File[]) {
    const images = files.filter((file) => file.type.startsWith("image/"));
    if (images.length === 0) return;
    setError(undefined);
    setUploading((count) => count + images.length);
    for (const file of images) {
      try {
        const attachment = await uploadAttachment(file, token);
        insertAtCursor(`\n![${attachment.filename}](${attachment.url})\n`);
      } catch (uploadError) {
        setError(uploadError instanceof Error ? uploadError.message : "Upload failed");
      } finally {
        setUploading((count) => count - 1);
      }
    }
  }

  return (
    <div className={cn(grow && "flex min-h-0 flex-1 flex-col")}>
      {editing ? (
        <div
          className={cn("flex min-h-0 flex-col", grow && "flex-1")}
          onPointerDown={() => {
            pointerInside.current = true;
          }}
          onBlur={(event) => {
            const inside = pointerInside.current || event.currentTarget.contains(event.relatedTarget as Node | null);
            pointerInside.current = false;
            if (!inside && !startEditing && value.trim() !== "") setEditing(false);
          }}
        >
          <Textarea
            ref={areaRef}
            className={cn(
              "mt-1 min-h-28",
              bare && "min-h-20 resize-none rounded-none border-0 bg-transparent px-0 py-0 focus:border-0 focus:ring-0",
              // The editor keeps the larger share: the preview is for checking
              // what was written, the textarea is where the writing happens.
              grow && "scrollbar-subtle min-h-0 flex-[3]"
            )}
            value={value}
            placeholder={placeholder}
            autoFocus={!bare && value.trim() !== ""}
            onChange={(event) => onChange(event.target.value)}
            onPaste={(event) => {
              const files = Array.from(event.clipboardData?.files ?? []);
              if (files.some((file) => file.type.startsWith("image/"))) {
                event.preventDefault();
                void upload(files);
              }
            }}
            onDrop={(event) => {
              const files = Array.from(event.dataTransfer?.files ?? []);
              if (files.some((file) => file.type.startsWith("image/"))) {
                event.preventDefault();
                void upload(files);
              }
            }}
          />
          {value.trim() ? (
            <div
              className={cn(
                "mt-2 rounded-md border border-border/60 bg-surface-2/20 p-2",
                grow ? "scrollbar-subtle min-h-0 flex-[2] overflow-y-auto" : "max-h-72 overflow-y-auto"
              )}
            >
              <span className="mb-1 block text-[10px] font-medium uppercase tracking-wide text-muted">Preview</span>
              <IssueMarkdown text={value} />
            </div>
          ) : null}
        </div>
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

      <div className="mt-1.5 flex shrink-0 flex-wrap items-center gap-2 text-[11px] text-muted">
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded px-1 py-0.5 hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65"
          onClick={() => {
            setEditing(true);
            fileRef.current?.click();
          }}
        >
          <ImagePlus className="h-3 w-3" aria-hidden />
          Add image
        </button>
        <span className={cn(uploading > 0 ? "inline-flex items-center gap-1" : "hidden")}>
          <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
          Uploading {uploading}…
        </span>
        {uploading === 0 && !error && !bare ? (
          <span>Paste or drop a screenshot. PNG, JPEG, GIF or WebP, up to 10MB.</span>
        ) : null}
        {error ? <span className="text-danger">{error}</span> : null}
      </div>

      <input
        ref={fileRef}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        multiple
        className="hidden"
        onChange={(event) => {
          void upload(Array.from(event.target.files ?? []));
          event.target.value = "";
        }}
      />
    </div>
  );
}
