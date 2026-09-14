import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  }).format(date);
}

export function shortCommit(value?: string) {
  return value ? value.slice(0, 8) : "—";
}

const TIME_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 7],
  ["week", 4.35],
  ["month", 12],
  ["year", Infinity]
];

/** "3 minutes ago" for anything recent, an absolute date once it is old enough
 *  that the distance stops being the useful part. A conversation is read by how
 *  long ago each remark landed; a comment from last spring is read by when. */
export function formatTimeAgo(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  let delta = (date.getTime() - Date.now()) / 1000;
  if (Math.abs(delta) < 45) return "just now";
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  for (const [unit, step] of TIME_UNITS) {
    if (Math.abs(delta) < step) return formatter.format(Math.round(delta), unit);
    delta /= step;
    if (unit === "week") return formatDate(value);
  }
  return formatDate(value);
}
