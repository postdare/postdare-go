import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { fetchAttachmentBlob } from "../../api/postdareGo";
import { useAuthStore } from "../../store/auth";
import { cn } from "../../lib/utils";

/** Matches the URL the upload endpoint hands back, so only this server's own
 *  attachments take the authenticated path; anything else stays a plain image. */
export const ATTACHMENT_SRC = /^\/api\/v1\/attachments\/\d+$/;

/** Object URLs, held for the session instead of for one mount.
 *
 *  The field swaps between rendered markdown and an editor, so a per-mount
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
  // A description that shows the same image twice, on the read side and in the
  // editor at once, must not fetch the same bytes twice.
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
export function AttachmentImage({ src, alt, className }: { src: string; alt?: string; className?: string }) {
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
  return <img src={objectURL} alt={alt ?? ""} className={cn("max-h-80 rounded-md border border-border", className)} loading="lazy" />;
}

/** The <img> for a description, whichever side of the field is showing it: the
 *  server's own attachments come through authenticated, anything else is left
 *  to the browser. */
export function DescriptionImage({ src, alt, className }: { src?: string; alt?: string | null; className?: string }) {
  if (typeof src === "string" && ATTACHMENT_SRC.test(src)) {
    return <AttachmentImage src={src} alt={alt ?? undefined} className={className} />;
  }
  return (
    <img
      src={typeof src === "string" ? src : undefined}
      alt={alt ?? ""}
      className={cn("max-h-80 rounded-md border border-border", className)}
      loading="lazy"
    />
  );
}
