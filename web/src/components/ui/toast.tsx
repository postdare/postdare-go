import { useToastStore, type ToastTone } from "../../store/toast";

const toneClass: Record<ToastTone, string> = {
  default: "border-border bg-surface-2 text-ink",
  danger: "border-danger/35 bg-danger/15 text-danger"
};

/** The app's single stack of confirmations.
 *
 *  Rendered once in the shell rather than next to each trigger, so a
 *  confirmation survives the card that caused it unmounting -- the board
 *  refetches on every change -- and announces itself beside the layout instead
 *  of inside it. It sits above the mobile tab bar the same way the page
 *  content does.
 */
export function Toaster() {
  const toasts = useToastStore((state) => state.toasts);

  if (toasts.length === 0) return null;

  return (
    <div
      className="pointer-events-none fixed bottom-[calc(4.5rem+env(safe-area-inset-bottom))] right-4 z-50 flex flex-col items-end gap-2 md:bottom-5 md:right-6"
      role="status"
      aria-live="polite"
    >
      {toasts.map((item) => (
        <p
          key={item.id}
          className={`rounded-md border px-3 py-2 text-sm shadow-lg shadow-black/20 ${toneClass[item.tone]}`}
        >
          {item.message}
        </p>
      ))}
    </div>
  );
}
