import type { ReactNode } from "react";
import { ThemeButton } from "./ThemeButton";

/** The strip over the content: the theme button now, and from #6 the signed-in person beside it. */
export function TopBar({ trailing }: { trailing?: ReactNode }) {
  return (
    <header className="flex h-12 shrink-0 items-center justify-end gap-2 border-b border-border bg-surface px-4" data-top-bar>
      <ThemeButton />
      {trailing}
    </header>
  );
}
