import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { JoinRequests } from "./JoinRequests";
import { UserTable, type UsersShown } from "./UserTable";

/** Admin's Users page: who waits to be let in, then everybody let in, found by search, a page at a time. */
export function UsersPage({ shown, show }: { shown: UsersShown; show: (next: UsersShown, replace?: boolean) => void }) {
  return (
    <div>
      <PageHeader title={s.users.title} />
      <p className="mb-4 max-w-3xl text-sm text-ink-muted">{s.users.intro}</p>
      <JoinRequests />
      <UserTable q={shown.q} offset={shown.offset} show={show} />
    </div>
  );
}
