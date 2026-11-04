import type { ReactNode } from "react";
import { PageHeader } from "@/features/shell/EmptyState";
import { card } from "./styles";

type DataAttributes = { [key: `data-${string}`]: string | number | boolean | undefined };

/** A section of a Settings or Admin page: a titled card, as wide as one grid cell or, wide, the whole row. */
export function SettingsCard({
  titleId,
  title,
  description,
  descriptionId,
  wide,
  className = "",
  children,
  ...data
}: {
  titleId: string;
  title: string;
  description?: string;
  descriptionId?: string;
  wide?: boolean;
  className?: string;
  children?: ReactNode;
} & DataAttributes) {
  return (
    <section className={`${card} min-w-0 ${wide ? "lg:col-span-full" : ""} ${className}`} aria-labelledby={titleId} {...data}>
      <h2 id={titleId} className={`${description ? "mb-1" : "mb-3"} text-lg font-semibold text-ink`}>
        {title}
      </h2>
      {description && (
        <p id={descriptionId} className="mb-3 text-sm text-ink-muted">
          {description}
        </p>
      )}
      {children}
    </section>
  );
}

/** The cards of a page: one column, two from lg and three from 2xl. */
export function SettingsGrid({ children }: { children: ReactNode }) {
  return <div className="grid items-start gap-4 lg:grid-cols-2 2xl:grid-cols-3">{children}</div>;
}

/** A Settings or Admin page: its heading, the page's one h1, over its cards. */
export function SettingsPage({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <PageHeader title={title} />
      <SettingsGrid>{children}</SettingsGrid>
    </div>
  );
}
