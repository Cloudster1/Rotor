import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { UserTable, type UsersShown } from "./UserTable";

/** Admin's Users page: everybody who has signed in, found by search, a page at a time. */
export function UsersPage({ shown, show }: { shown: UsersShown; show: (next: UsersShown, replace?: boolean) => void }) {
  return (
    <div>
      <PageHeader title={s.users.title} />
      <p className="mb-4 max-w-3xl text-sm text-ink-muted">{s.users.intro}</p>
      <UserTable q={shown.q} offset={shown.offset} show={show} />
    </div>
  );
}
