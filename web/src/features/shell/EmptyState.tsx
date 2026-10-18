import type { ReactNode } from "react";

/** A page's heading, the one h1 a page has. */
export function PageHeader({ title }: { title: string }) {
  return <h1 className="mb-6 text-xl font-semibold text-ink">{title}</h1>;
}

/** What a page shows when it has nothing yet: what will be here, and how it gets here. */
export function EmptyState({ icon, title, description, action }: { icon: ReactNode; title: string; description: string; action?: ReactNode }) {
  return (
    <section
      className="mx-auto flex max-w-md flex-col items-center gap-3 rounded-overlay border border-border bg-surface px-6 py-10 text-center shadow-1"
      data-empty-state
    >
      <span className="flex size-10 items-center justify-center rounded-full bg-surface-raised text-ink-muted">{icon}</span>
      <h2 className="text-lg font-semibold text-ink">{title}</h2>
      <p className="text-sm text-ink-muted">{description}</p>
      {action}
    </section>
  );
}
