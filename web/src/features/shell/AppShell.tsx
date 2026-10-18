import type { MouseEvent, ReactNode } from "react";
import { s } from "@/strings";
import { Sidebar } from "./Sidebar";
import { TopBar } from "./TopBar";

const MAIN_ID = "main";

/** The chrome every page sits in: skip link, sidebar, top bar and the content. */
export function AppShell({ children, topBarTrailing }: { children: ReactNode; topBarTrailing?: ReactNode }) {
  return (
    <div className="flex h-full flex-col shell:flex-row">
      <a
        href={`#${MAIN_ID}`}
        onClick={skipToMain}
        className="sr-only z-50 rounded-control bg-surface-overlay px-3 py-2 text-sm font-medium text-ink shadow-2 focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
        data-skip-link
      >
        {s.shell.skipToContent}
      </a>
      <Sidebar />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <TopBar trailing={topBarTrailing} />
        {/* Focusable only from the skip link, and not ringed then: it is where reading starts, not a control. */}
        <main id={MAIN_ID} tabIndex={-1} className="min-h-0 flex-1 overflow-auto bg-backdrop px-4 py-6 focus:outline-none shell:px-8">
          {children}
        </main>
      </div>
    </div>
  );
}

// The router owns the address, so the skip link moves focus itself rather than changing the hash.
function skipToMain(event: MouseEvent<HTMLAnchorElement>) {
  event.preventDefault();
  document.getElementById(MAIN_ID)?.focus();
}
