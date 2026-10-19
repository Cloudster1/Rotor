import { Link } from "@tanstack/react-router";
import { APP_NAME, MARK_WIDTH_PX } from "@/config";
import { ApprovalBadge } from "@/features/approvals/ApprovalBadge";
import { s } from "@/strings";
import { Icon } from "./icons";
import { PLACES } from "./places";

/** The product's mark and every place, beside the content on a wide screen and above it on a narrow one. */
export function Sidebar() {
  return (
    <aside className="flex shrink-0 flex-col gap-4 border-b border-border bg-surface px-3 py-3 shell:w-56 shell:border-r shell:border-b-0 shell:py-4">
      <Link to="/workflows" className="flex items-center gap-2 px-2 text-base font-semibold text-ink" aria-label={s.shell.home}>
        <img src="/favicon.svg" alt="" width={MARK_WIDTH_PX} height={MARK_WIDTH_PX} />
        <span>{APP_NAME}</span>
      </Link>
      <nav aria-label={s.shell.navigation}>
        <ul className="flex gap-1 overflow-x-auto shell:flex-col">
          {PLACES.map((place) => {
            const Glyph = Icon[place.icon];
            return (
              <li key={place.path}>
                <Link
                  to={place.path}
                  data-place={place.path}
                  className="flex h-8 items-center gap-2 rounded-control px-2 text-sm whitespace-nowrap text-ink-muted hover:bg-surface-raised hover:text-ink"
                  activeProps={{ className: "bg-accent-subtle font-medium text-accent hover:bg-accent-subtle hover:text-accent" }}
                >
                  <Glyph className="shrink-0" />
                  <span>{place.label}</span>
                  {place.path === "/approvals" && <ApprovalBadge />}
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>
    </aside>
  );
}
