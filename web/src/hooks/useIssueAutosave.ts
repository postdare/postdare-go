import { useCallback, useEffect, useRef, useState } from "react";

import { updateIssue } from "../api/postdareGo";
import { useAuthStore } from "../store/auth";

/** What the header reports. "pending" is an edit that has not been sent yet,
 *  which is the state a debounce spends most of its time in. */
export type SaveState = "idle" | "pending" | "saving" | "saved" | "error";

/** Long enough that a sentence is one request rather than forty, short enough
 *  that looking away from the keyboard and back finds the work saved. */
const DEBOUNCE_MS = 700;

export interface IssueAutosave {
  state: SaveState;
  error: string | null;
  /** Records an edit. Typing is debounced; a picker passes `immediate` because
   *  choosing a value is a finished action, not a half-written one. */
  queue: (changes: Record<string, unknown>, options?: { immediate?: boolean }) => void;
  /** Sends whatever is queued right now. */
  flush: () => void;
  /** Drops what is queued. For the one case where the edits stop mattering:
   *  the issue they belong to is being deleted. */
  cancel: () => void;
}

/** Saves an issue as it is edited, so the page needs no save button.
 *
 *  Only the fields that changed are sent -- PATCH reads an absent field as
 *  "not part of this edit" -- and the changes are merged into one request, so
 *  a burst of typing is a single write and not one per keystroke.
 *
 *  A failed save keeps its changes: they go back on the queue rather than being
 *  dropped, so the next edit carries them along instead of silently losing what
 *  the author already typed.
 */
export function useIssueAutosave(issueID: string, options: { enabled?: boolean; onSaved?: () => void } = {}): IssueAutosave {
  const { enabled = true, onSaved } = options;
  const token = useAuthStore((state) => state.token);
  const [state, setState] = useState<SaveState>("idle");
  const [error, setError] = useState<string | null>(null);

  const queued = useRef<Record<string, unknown>>({});
  const timer = useRef<number | undefined>(undefined);
  // One request at a time. Two PATCHes in flight over the same row can land in
  // either order, and the loser would write a value the author has since
  // changed; anything queued meanwhile is picked up when this one returns.
  const sending = useRef(false);
  const latest = useRef({ issueID, token, enabled, onSaved });
  latest.current = { issueID, token, enabled, onSaved };

  const flush = useCallback(function flushQueue(init?: RequestInit) {
    window.clearTimeout(timer.current);
    if (sending.current || !latest.current.enabled) return;
    const changes = queued.current;
    if (Object.keys(changes).length === 0) return;
    queued.current = {};
    sending.current = true;
    setState("saving");
    updateIssue(latest.current.issueID, changes, latest.current.token, init)
      .then(() => {
        setError(null);
        latest.current.onSaved?.();
        sending.current = false;
        // An edit made while this request was away is sent now rather than
        // waiting for the next keystroke to notice it.
        if (Object.keys(queued.current).length > 0) {
          setState("pending");
          flushQueue();
          return;
        }
        setState("saved");
      })
      .catch((failure: unknown) => {
        queued.current = { ...changes, ...queued.current };
        setError(failure instanceof Error ? failure.message : "This edit could not be saved");
        setState("error");
        sending.current = false;
        // Deliberately not retried on a timer: a 422 would loop forever, and
        // the author is still here -- the next edit carries these changes.
      });
  }, []);

  const queue = useCallback<IssueAutosave["queue"]>(
    (changes, queueOptions) => {
      if (!latest.current.enabled) return;
      queued.current = { ...queued.current, ...changes };
      setState("pending");
      window.clearTimeout(timer.current);
      if (queueOptions?.immediate) {
        flush();
        return;
      }
      timer.current = window.setTimeout(() => flush(), DEBOUNCE_MS);
    },
    [flush]
  );

  // Leaving the page mid-debounce must not cost the last sentence. keepalive
  // lets the request outlive the document, which a plain fetch does not.
  useEffect(() => {
    function flushOnHide() {
      if (document.visibilityState === "hidden") flush({ keepalive: true });
    }
    document.addEventListener("visibilitychange", flushOnHide);
    return () => {
      document.removeEventListener("visibilitychange", flushOnHide);
      flush({ keepalive: true });
    };
  }, [flush]);

  const cancel = useCallback(() => {
    window.clearTimeout(timer.current);
    queued.current = {};
    setState("idle");
    setError(null);
  }, []);

  return { state, error, queue, flush: useCallback(() => flush(), [flush]), cancel };
}
