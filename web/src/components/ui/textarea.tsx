import * as React from "react";

import { cn } from "../../lib/utils";

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function Textarea({ className, ...props }, ref) {
    return (
      <textarea
        ref={ref}
        className={cn(
          "min-h-20 w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-ink outline-none transition-colors placeholder:text-muted focus:border-primary focus:ring-2 focus:ring-primary/30 disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
        {...props}
      />
    );
  }
);
