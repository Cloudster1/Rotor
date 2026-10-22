import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId } from "react";
import { setUnattendedWrites, triggersQueryKey, unattendedSettingQuery, unattendedSettingQueryKey } from "@/api/triggers";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";

/** Whether triggers may write through the calls their owners agreed to with no approval, for administrators (#128). */
export function UnattendedWritesSection() {
  const id = useId();
  const client = useQueryClient();
  const setting = useQuery(unattendedSettingQuery);
  const toggle = useMutation({
    mutationFn: setUnattendedWrites,
    onSuccess: async (next) => {
      client.setQueryData(unattendedSettingQueryKey, next);
      await client.invalidateQueries({ queryKey: triggersQueryKey });
    },
  });
  const failed = setting.error ?? toggle.error;
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-unattended-writes>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {s.triggers.unattendedSetting.title}
      </h2>
      <p id={`${id}-hint`} className="mb-3 text-sm text-ink-muted">
        {s.triggers.unattendedSetting.body}
      </p>
      {setting.data && (
        <label className="flex items-center gap-2 text-sm font-medium text-ink">
          <input
            type="checkbox"
            checked={setting.data.allowed}
            disabled={toggle.isPending}
            onChange={(e) => toggle.mutate(e.target.checked)}
            aria-describedby={`${id}-hint`}
            data-unattended-allowed
          />
          {s.triggers.unattendedSetting.allowed}
        </label>
      )}
      {failed && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {failed.message}
        </p>
      )}
    </section>
  );
}
