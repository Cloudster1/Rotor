import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { accessQueryKey } from "@/api/access";
import { admitJoinRequest, declineJoinRequest, type JoinRequest, joinRequestsQuery } from "@/api/users";
import { Refusal } from "@/features/access/ui";
import { card, primary, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

function asked(at: string): string {
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/**
 * Who signed in through the identity provider and waits to be let in, above Admin's Users table; it takes no room
 * while nobody waits. Ported from Armature's JoinRequests.
 */
export function JoinRequests() {
  const client = useQueryClient();
  const requests = useQuery(joinRequestsQuery);
  const [said, setSaid] = useState("");
  const settled = () => client.invalidateQueries({ queryKey: accessQueryKey });
  const admit = useMutation({
    mutationFn: (request: JoinRequest) => admitJoinRequest(request.userId),
    onSuccess: (_, request) => {
      setSaid(s.joinRequests.letIn(request.name || request.email));
      return settled();
    },
  });
  const decline = useMutation({
    mutationFn: (request: JoinRequest) => declineJoinRequest(request.userId),
    onSuccess: (_, request) => {
      setSaid(s.joinRequests.turnedAway(request.name || request.email));
      return settled();
    },
  });
  const waiting = requests.data ?? [];

  return (
    <>
      {/* Only once there is news, so the Users table's own status line stays the one on the page until then. */}
      {said && (
        <p role="status" className="sr-only" data-join-requests-said>
          {said}
        </p>
      )}
      {waiting.length > 0 && (
        <section className={`${card} mb-4`} aria-labelledby="join-requests-title" data-join-requests>
          <h2 id="join-requests-title" className="text-base font-semibold text-ink">
            {s.joinRequests.title}
          </h2>
          <p className="mt-1 mb-3 text-sm text-ink-muted">{s.joinRequests.detail}</p>
          <Refusal error={admit.error ?? decline.error} />
          <ul className="flex flex-col divide-y divide-border">
            {waiting.map((request) => {
              const name = request.name || request.email;
              const busy =
                (admit.isPending && admit.variables?.userId === request.userId) || (decline.isPending && decline.variables?.userId === request.userId);
              return (
                <li key={request.userId} className="flex flex-wrap items-center justify-between gap-2 py-2" data-join-request={request.email}>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-ink">{name}</p>
                    <p className="text-xs text-ink-muted">{s.joinRequests.asked(request.email, asked(request.requestedAt))}</p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      className={secondary}
                      disabled={busy}
                      aria-label={s.joinRequests.turnAwayNamed(name)}
                      onClick={() => decline.mutate(request)}
                      data-action="decline-request"
                    >
                      {s.joinRequests.turnAway}
                    </button>
                    <button
                      type="button"
                      className={`${primary} h-8`}
                      disabled={busy}
                      aria-label={s.joinRequests.admitNamed(name)}
                      onClick={() => admit.mutate(request)}
                      data-action="admit-request"
                    >
                      {s.joinRequests.admit}
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </>
  );
}
