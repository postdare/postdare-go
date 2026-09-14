import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "../../lib/utils";

const buttonVariants = cva(
  "inline-flex h-9 items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/65 disabled:pointer-events-none disabled:opacity-45",
  {
    variants: {
      variant: {
        primary: "bg-primary text-primary-ink hover:bg-primary/90",
        secondary: "border border-border bg-surface text-ink hover:bg-surface-2",
        ghost: "text-muted hover:bg-surface-2 hover:text-ink",
        danger: "bg-danger text-white hover:bg-danger/90"
      },
      size: {
        sm: "h-9 px-2.5 text-xs",
        md: "h-10 px-3",
        icon: "h-10 w-10 px-0"
      }
    },
    defaultVariants: {
      variant: "secondary",
      size: "md"
    }
  }
);

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {}

/** Forwards its ref so Radix can use a Button as its own trigger: `asChild`
 *  hands the child the ref it anchors a menu or dialog to, and a plain function
 *  component silently drops it, leaving a trigger that never opens. */
export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, ...props },
  ref
) {
  return <button ref={ref} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
});
