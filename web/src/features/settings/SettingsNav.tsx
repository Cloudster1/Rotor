import { Link } from "@tanstack/react-router";
import type { SettingsNavGroup } from "./groups";

/** The pages of Settings, Admin or a workflow's settings, beside the content from lg and a row that scrolls sideways above it below. */
export function SettingsNav({ label, groups }: { label: string; groups: readonly SettingsNavGroup[] }) {
  return (
    <nav aria-label={label} className="min-w-0 border-b border-border pb-2 lg:w-settings-nav lg:shrink-0 lg:border-b-0 lg:pb-0">
      <div className="flex gap-1 overflow-x-auto lg:flex-col lg:gap-4 lg:overflow-visible">
        {groups.map((group) => {
          const titleId = `settings-nav-${group.id}`;
          return (
            <div key={group.id} className="flex shrink-0 flex-col gap-1">
              {/* In the sideways row the headings would only crowd it; a screen reader still hears them. */}
              {group.title && (
                <p id={titleId} className="sr-only px-2 text-xs font-semibold tracking-wide text-ink-muted uppercase lg:not-sr-only">
                  {group.title}
                </p>
              )}
              <ul className="flex gap-1 lg:flex-col" aria-labelledby={group.title ? titleId : undefined}>
                {group.pages.map((page) => (
                  <li key={page.path}>
                    <Link
                      to={page.path}
                      params={page.params}
                      data-settings-page={page.slug}
                      className="flex h-8 items-center rounded-control px-2 text-sm whitespace-nowrap text-ink-muted hover:bg-surface-raised hover:text-ink"
                      activeProps={{ className: "bg-accent-subtle font-medium text-accent hover:bg-accent-subtle hover:text-accent" }}
                    >
                      {page.label}
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </nav>
  );
}
