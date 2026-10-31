import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { ApiError } from "@/api/client";
import { startConnection } from "@/api/connections";
import { runsQueryKey, startRun } from "@/api/runs";
import { workflowQuery } from "@/api/workflows";
import { s } from "@/strings";
import { RunsOff, useRunsOn } from "./RunsOff";
import { fieldsOf, InputsForm, inputsOf, type Values, WHOLE } from "./InputsForm";

const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** The tools a refused start names, by name, with their ids; an empty id is a tool Rotor does not have. */
function missingTools(error: unknown): [string, string][] {
  return error instanceof ApiError && error.code === "connections_needed" ? Object.entries(error.fields) : [];
}

/** The server's word on each field, from a refused start's "inputs/<field>" keys. */
function serverErrors(error: unknown): Record<string, string> {
  if (!(error instanceof ApiError) || error.code !== "inputs_invalid") return {};
  const out: Record<string, string> = {};
  for (const [key, message] of Object.entries(error.fields)) {
    const field = key.replace(/^inputs\/?/, "").split("/")[0] ?? WHOLE;
    out[field] ??= message;
  }
  return out;
}

/** Starting a run of a workflow: its inputs, the tools it acts in as you, and connecting those you have not. */
export function RunDialog({ workflowID, name, onClose }: { workflowID: string; name: string; onClose: () => void }) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  const navigate = useNavigate();
  const client = useQueryClient();
  const workflow = useQuery(workflowQuery(workflowID));
  const runsOn = useRunsOn();
  const version = workflow.data?.version;
  const fields = useMemo(() => fieldsOf(version?.inputs ?? null), [version]);
  const [values, setValues] = useState<Values>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  // One key per opening: a second click while the first is on its way starts nothing twice.
  const [key] = useState(() => crypto.randomUUID());

  useEffect(() => {
    const element = dialog.current;
    if (element && !element.open) element.showModal();
    return () => element?.close();
  }, []);

  const start = useMutation({
    mutationFn: (inputs: Record<string, unknown>) => startRun(workflowID, inputs, key),
    onSuccess: async (run) => {
      await client.invalidateQueries({ queryKey: runsQueryKey });
      onClose();
      await navigate({ to: "/runs/$runID", params: { runID: run.id } });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const { inputs, errors: found } = inputsOf(fields, values);
    setErrors(found);
    if (Object.keys(found).length === 0) start.mutate(inputs);
  }

  async function connect(toolID: string) {
    window.location.assign(await startConnection(toolID, `/workflows/${workflowID}`));
  }

  const missing = missingTools(start.error);
  const shown = { ...serverErrors(start.error), ...errors };
  const refused = start.isError && missing.length === 0 && Object.keys(serverErrors(start.error)).length === 0;
  return (
    <dialog
      ref={dialog}
      onClose={onClose}
      aria-labelledby={`${id}-title`}
      className="m-auto w-[min(36rem,calc(100vw-2rem))] rounded-overlay border border-border bg-surface-overlay p-0 text-ink shadow-2 backdrop:bg-black/40"
      data-run-dialog
    >
      <form onSubmit={submit} className="flex max-h-[85vh] flex-col gap-4 overflow-y-auto p-5" noValidate>
        <h2 id={`${id}-title`} className="text-lg font-semibold text-ink">
          {s.runs.dialog.title(name)}
        </h2>
        {!runsOn && <RunsOff />}
        <p className="text-sm text-ink-muted">{s.runs.dialog.asYou}</p>
        {version && version.bindings.length > 0 && (
          <div className="text-sm text-ink">
            <span className="font-medium">{s.runs.dialog.uses}: </span>
            {version.bindings.map((b) => `${b.tool} (${s.runs.dialog.access[b.access as "read" | "write"] ?? b.access})`).join(", ")}
          </div>
        )}
        {workflow.isPending ? (
          <p className="text-sm text-ink-muted">{s.workflows.loading}</p>
        ) : (
          <InputsForm fields={fields} values={values} errors={shown} onChange={(field, value) => setValues((v) => ({ ...v, [field]: value }))} />
        )}
        {missing.length > 0 && (
          <div
            role="alert"
            className="flex flex-col gap-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink"
            data-connect-first
          >
            <p>{s.runs.dialog.connectFirst}</p>
            <ul className="flex flex-wrap gap-2">
              {missing.map(([tool, toolID]) => (
                <li key={tool}>
                  {toolID ? (
                    <button type="button" className={secondary} onClick={() => connect(toolID)} data-connect={tool}>
                      {s.connections.connect(tool)}
                    </button>
                  ) : (
                    <span>{s.runs.dialog.missingTool(tool)}</span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
        {refused && (
          <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-run-error>
            {start.error.message}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className={secondary} onClick={onClose}>
            {s.runs.dialog.cancel}
          </button>
          <button type="submit" className={primary} disabled={start.isPending || workflow.isPending || !runsOn} data-action="start-run">
            {start.isPending ? s.runs.dialog.starting : s.runs.dialog.start}
          </button>
        </div>
      </form>
    </dialog>
  );
}
