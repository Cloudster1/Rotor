import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { meQuery, signOut } from "@/api/auth";
import { LOGIN_PATH } from "@/config";
import { s } from "@/strings";

/** The signed-in person in the top bar, and the way out. */
export function AccountControl() {
  const me = useQuery(meQuery);
  const client = useQueryClient();
  const navigate = useNavigate();
  const [leaving, setLeaving] = useState(false);

  async function leave() {
    setLeaving(true);
    try {
      await signOut();
    } finally {
      client.clear();
      await navigate({ to: LOGIN_PATH, search: {} });
    }
  }

  return (
    <div className="flex items-center gap-2">
      {me.data && (
        <span className="hidden text-sm text-ink-muted sm:inline" data-signed-in-as>
          {me.data.user.name}
        </span>
      )}
      <button
        type="button"
        onClick={leave}
        disabled={leaving}
        className="h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60"
        data-action="sign-out"
      >
        {s.signIn.signOut}
      </button>
    </div>
  );
}
