import { type KeyboardEvent, useId, useRef } from "react";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { GroupList } from "./GroupList";
import { ProviderSettings } from "./ProviderSettings";
import { RoleMatrix } from "./RoleMatrix";
import { RoleTable } from "./RoleTable";

/** The tabs of Access, as the address names them; Single sign-on joined them with #204. */
export const ACCESS_TABS = ["roles", "matrix", "groups", "sso"] as const;
export type AccessTab = (typeof ACCESS_TABS)[number];

const tabLabel: Record<AccessTab, string> = {
  roles: s.access.tabs.roles,
  matrix: s.access.tabs.matrix,
  groups: s.access.tabs.groups,
  sso: s.access.tabs.sso,
};

/** Admin's Access page: who holds which role, what each role grants, the groups and single sign-on. Ported from Armature's. */
export function AccessPage({ tab, onTab }: { tab: AccessTab; onTab: (tab: AccessTab) => void }) {
  const id = useId();
  const buttons = useRef<Partial<Record<AccessTab, HTMLButtonElement | null>>>({});

  // Arrow keys move between the tabs, as the tabs pattern of the ARIA practices has them.
  function keyed(event: KeyboardEvent) {
    const at = ACCESS_TABS.indexOf(tab);
    const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    const to =
      event.key === "Home"
        ? ACCESS_TABS[0]
        : event.key === "End"
          ? ACCESS_TABS[ACCESS_TABS.length - 1]
          : step
            ? ACCESS_TABS[(at + step + ACCESS_TABS.length) % ACCESS_TABS.length]
            : undefined;
    if (!to) return;
    event.preventDefault();
    onTab(to);
    buttons.current[to]?.focus();
  }

  return (
    <div>
      <PageHeader title={s.access.title} />
      <div role="tablist" aria-label={s.access.tabs.label} className="mb-4 flex gap-1 overflow-x-auto border-b border-border" onKeyDown={keyed}>
        {ACCESS_TABS.map((each) => (
          <button
            key={each}
            ref={(element) => {
              buttons.current[each] = element;
            }}
            type="button"
            role="tab"
            id={`${id}-${each}-tab`}
            aria-selected={tab === each}
            aria-controls={`${id}-${each}`}
            tabIndex={tab === each ? 0 : -1}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium whitespace-nowrap ${tab === each ? "border-accent text-ink" : "border-transparent text-ink-muted hover:text-ink"}`}
            onClick={() => onTab(each)}
            data-tab={each}
          >
            {tabLabel[each]}
          </button>
        ))}
      </div>
      {ACCESS_TABS.map((each) => (
        <div key={each} role="tabpanel" id={`${id}-${each}`} aria-labelledby={`${id}-${each}-tab`} hidden={tab !== each} data-access-tab={each}>
          {tab === each && each === "roles" && <RoleTable />}
          {tab === each && each === "matrix" && <RoleMatrix />}
          {tab === each && each === "groups" && <GroupList />}
          {tab === each && each === "sso" && <ProviderSettings />}
        </div>
      ))}
    </div>
  );
}
