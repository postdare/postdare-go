import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useState } from "react";
import { Plus, Tag, X } from "lucide-react";

import { cn } from "../../lib/utils";

/** Labels are free text, so there is no stored colour to read. The dot colour is
 *  derived from the name instead: the same label is always the same colour, on
 *  every card and every board, without a colour column to keep in sync. */
const LABEL_DOTS = ["bg-danger", "bg-warning", "bg-success", "bg-info", "bg-accent", "bg-primary"];

export function labelDot(label: string) {
  let hash = 0;
  const key = label.trim().toLowerCase();
  for (let i = 0; i < key.length; i += 1) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return LABEL_DOTS[hash % LABEL_DOTS.length];
}

export function LabelChip({
  label,
  onRemove,
  className
}: {
  label: string;
  onRemove?: () => void;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex h-6 max-w-full items-center gap-1.5 rounded-full border border-border bg-surface pl-2 text-xs text-ink",
        onRemove ? "pr-1" : "pr-2",
        className
      )}
    >
      <span aria-hidden className={cn("h-1.5 w-1.5 shrink-0 rounded-full", labelDot(label))} />
      <span className="truncate">{label}</span>
      {onRemove ? (
        <button
          type="button"
          className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-muted transition-colors hover:bg-surface-2 hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65"
          aria-label={`Remove label ${label}`}
          onClick={onRemove}
        >
          <X className="h-3 w-3" />
        </button>
      ) : null}
    </span>
  );
}

function AddLabelMenu({
  value,
  suggestions,
  onChange,
  trigger
}: {
  value: string[];
  suggestions: string[];
  onChange: (next: string[]) => void;
  trigger: React.ReactNode;
}) {
  const [query, setQuery] = useState("");
  const taken = new Set(value.map((label) => label.toLowerCase()));
  const term = query.trim();
  const matches = suggestions
    .filter((label) => !taken.has(label.toLowerCase()))
    .filter((label) => (term ? label.toLowerCase().includes(term.toLowerCase()) : true))
    .slice(0, 8);
  const canCreate = term !== "" && !taken.has(term.toLowerCase()) && !suggestions.some((l) => l.toLowerCase() === term.toLowerCase());

  function add(label: string) {
    const trimmed = label.trim();
    if (!trimmed || taken.has(trimmed.toLowerCase()) || value.length >= 10) return;
    onChange([...value, trimmed]);
    setQuery("");
  }

  return (
    <DropdownMenu.Root onOpenChange={(open) => (open ? setQuery("") : undefined)}>
      <DropdownMenu.Trigger asChild>{trigger}</DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          className="z-50 w-56 overflow-hidden rounded-md border border-border bg-surface p-1 shadow-lg shadow-black/20"
          align="start"
          sideOffset={4}
        >
          <div className="p-1" onKeyDown={(event) => event.stopPropagation()}>
            <input
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Find or create a label"
              aria-label="Find or create a label"
              className="h-8 w-full rounded border border-border bg-background px-2 text-sm text-ink outline-none placeholder:text-muted focus:border-primary"
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  add(matches[0] && !canCreate ? matches[0] : query);
                }
              }}
            />
          </div>
          <div className="scrollbar-subtle max-h-56 overflow-y-auto">
            {matches.map((label) => (
              <DropdownMenu.Item
                key={label}
                className="flex h-8 cursor-pointer select-none items-center gap-2 rounded px-2 text-sm text-ink outline-none data-[highlighted]:bg-surface-2"
                onSelect={(event) => {
                  event.preventDefault();
                  add(label);
                }}
              >
                <span aria-hidden className={cn("h-1.5 w-1.5 rounded-full", labelDot(label))} />
                <span className="truncate">{label}</span>
              </DropdownMenu.Item>
            ))}
            {canCreate ? (
              <DropdownMenu.Item
                className="flex h-8 cursor-pointer select-none items-center gap-2 rounded px-2 text-sm text-ink outline-none data-[highlighted]:bg-surface-2"
                onSelect={(event) => {
                  event.preventDefault();
                  add(query);
                }}
              >
                <Plus className="h-3 w-3 text-muted" aria-hidden />
                <span className="truncate">
                  Create <span className="font-medium">{term}</span>
                </span>
              </DropdownMenu.Item>
            ) : null}
            {matches.length === 0 && !canCreate ? (
              <p className="px-2 py-3 text-center text-xs text-muted">
                {suggestions.length === 0 ? "No labels on this board yet" : "Nothing left to add"}
              </p>
            ) : null}
          </div>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

/** "inline" lays the chips out in the properties panel with a + beside them;
 *  "pill" is the composer's single control, which grows into the chips once
 *  labels are picked. */
export function LabelsEditor({
  value,
  suggestions,
  onChange,
  layout = "inline"
}: {
  value: string[];
  suggestions: string[];
  onChange: (next: string[]) => void;
  layout?: "inline" | "pill";
}) {
  const remove = (label: string) => onChange(value.filter((current) => current !== label));

  if (layout === "pill" && value.length === 0) {
    return (
      <AddLabelMenu
        value={value}
        suggestions={suggestions}
        onChange={onChange}
        trigger={
          <button
            type="button"
            className="inline-flex h-8 items-center gap-1.5 rounded-full border border-border bg-surface px-2.5 text-xs text-muted transition-colors hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 data-[state=open]:bg-surface-2"
          >
            <Tag className="h-3.5 w-3.5" aria-hidden />
            Labels
          </button>
        }
      />
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {value.map((label) => (
        <LabelChip key={label} label={label} onRemove={() => remove(label)} />
      ))}
      <AddLabelMenu
        value={value}
        suggestions={suggestions}
        onChange={onChange}
        trigger={
          <button
            type="button"
            className="inline-flex h-6 items-center gap-1 rounded-full border border-dashed border-border px-2 text-xs text-muted transition-colors hover:border-primary/40 hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 data-[state=open]:bg-surface-2"
            aria-label="Add label"
          >
            <Plus className="h-3 w-3" aria-hidden />
            {value.length === 0 ? "Label" : null}
          </button>
        }
      />
    </div>
  );
}
