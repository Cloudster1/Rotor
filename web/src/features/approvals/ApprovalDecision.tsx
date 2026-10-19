import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useMemo, useState } from "react";
import { type Approval, type Decision, approvalQuery, approvalsQueryKey, decideApproval } from "@/api/approvals";
import { ApiError } from "@/api/client";
import { runsQueryKey } from "@/api/runs";
import { HTTP_UNPROCESSABLE, MAX_APPROVAL_COMMENT } from "@/config";
import { fieldsOf, InputsForm, inputsOf, type Values, valuesOf, WHOLE } from "@/features/runs/InputsForm";
import { s } from "@/strings";

const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const danger = "h-9 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60";
const area = "min-h-20 w-full rounded-control border border-border-strong bg-surface px-3 py-2 text-sm text-ink";

/** The server's word on each changed value, from a refused decision's "values/<field>" keys. */
function serverErrors(error: unknown): Record<string, string> {
  if (!(error instanceof ApiError) || error.status !== HTTP_UNPROCESSABLE) return {};
  const out: Record<string, string> = {};
  for (const [key, message] of Object.entries(error.fields)) {
    if (!key.startsWith("values")) continue;
    const field = key.replace(/^values\/?/, "").split("/")[0] ?? WHOLE;
    out[field] ??= message;
  }
  return out;
}

/** Deciding a pending approval: the fields the node lets an approver change, a comment, approve or reject. */
export function ApprovalDecision({ approval }: { approval: Approval }) {
  const id = useId();
  const client = useQueryClient();
  const schema = approval.form?.schema ?? null;
  const fields = useMemo(() => (schema === null ? [] : fieldsOf(schema)), [schema]);
  const [values, setValues] = useState<Values>(() => valuesOf(fields, approval.form?.values as Record<string, unknown> | null));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [comment, setComment] = useState("");
  const [confirming, setConfirming] = useState(false);

  const decide = useMutation({
    mutationFn: (decision: Decision) => decideApproval(approval.id, decision),
    onSuccess: (decided) => {
      client.setQueryData(approvalQuery(approval.id).queryKey, decided);
      void client.invalidateQueries({ queryKey: approvalsQueryKey });
      void client.invalidateQueries({ queryKey: runsQueryKey });
    },
  });

  function approve(event: FormEvent) {
    event.preventDefault();
    const { inputs, errors: found } = inputsOf(fields, values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    decide.mutate({ decision: "approve", comment: comment || undefined, values: approval.form ? inputs : undefined });
  }

  const fieldErrors = { ...serverErrors(decide.error), ...errors };
  const refused = decide.isError && Object.keys(serverErrors(decide.error)).length === 0;
  const t = s.approvals.page;
  return (
    <form onSubmit={approve} className="flex max-w-2xl flex-col gap-4" noValidate data-approval-decision>
      {approval.form && (
        <section aria-labelledby={`${id}-form`} className="flex flex-col gap-3">
          <h2 id={`${id}-form`} className="text-lg font-semibold text-ink">
            {t.form}
          </h2>
          <InputsForm fields={fields} values={values} errors={fieldErrors} onChange={(field, value) => setValues((v) => ({ ...v, [field]: value }))} />
        </section>
      )}
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-comment`} className="text-sm font-medium text-ink">
          {t.comment}
        </label>
        <textarea
          id={`${id}-comment`}
          className={area}
          value={comment}
          maxLength={MAX_APPROVAL_COMMENT}
          aria-describedby={`${id}-comment-hint`}
          onChange={(e) => setComment(e.target.value)}
          data-approval-comment
        />
        <p id={`${id}-comment-hint`} className="text-xs text-ink-muted">
          {t.commentHint}
        </p>
      </div>
      {refused && (
        <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-approval-error>
          {decide.error.message}
        </p>
      )}
      {confirming ? (
        <div role="group" aria-label={t.confirmReject} className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-ink">{t.confirmReject}</span>
          <button
            type="button"
            className={danger}
            disabled={decide.isPending}
            onClick={() => decide.mutate({ decision: "reject", comment: comment || undefined })}
            data-action="confirm-reject"
          >
            {decide.isPending ? t.rejecting : t.confirm}
          </button>
          <button type="button" className={secondary} onClick={() => setConfirming(false)}>
            {t.keep}
          </button>
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          <button type="submit" className={primary} disabled={decide.isPending} data-action="approve">
            {decide.isPending ? t.approving : t.approve}
          </button>
          <button type="button" className={secondary} disabled={decide.isPending} onClick={() => setConfirming(true)} data-action="reject">
            {t.reject}
          </button>
        </div>
      )}
    </form>
  );
}
