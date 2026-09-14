import { create } from "zustand";

export type ToastTone = "default" | "danger";

export interface Toast {
  id: number;
  message: string;
  tone: ToastTone;
}

/** Long enough to read three words, short enough that repeatedly copying does
 *  not pile confirmations up in the corner. */
const TOAST_MS = 1800;

let nextID = 1;

interface ToastState {
  toasts: Toast[];
  push: (message: string, tone: ToastTone) => void;
  dismiss: (id: number) => void;
}

export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],
  push: (message, tone) => {
    const id = nextID++;
    set((state) => ({ toasts: [...state.toasts, { id, message, tone }] }));
    window.setTimeout(() => get().dismiss(id), TOAST_MS);
  },
  dismiss: (id) => set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) }))
}));

/** Fire-and-forget confirmation, callable from anywhere including event
 *  handlers that cannot reach the store's hook. A refused write reports itself
 *  here too, so the tone carries whether the action landed. */
export function toast(message: string, tone: ToastTone = "default") {
  useToastStore.getState().push(message, tone);
}
