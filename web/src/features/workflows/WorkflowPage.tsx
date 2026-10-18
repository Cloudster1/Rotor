import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type ChangeEvent, useEffect, useId, useRef, useState } from "react";
import {
  createWorkflow,
  type Problem,
  saveWorkflow,
  schemaQuery,
  validateWorkflow,
  type WorkflowResponse,
  workflowQuery,
  workflowsQueryKey,
} from "@/api/workflows";
import { MAX_VERSION_COMMENT, VALIDATE_DELAY_MS } from "@/config";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { SharePanel } from "./SharePanel";
import { ToolPicker } from "./ToolPicker";
import { NEW_WORKFLOW, fileName } from "./template";
import { VersionsPanel } from "./VersionsPanel";
import { YamlEditor, type YamlEditorHandle } from "./YamlEditor";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** An existing workflow, once loaded; the editor starts over when another version becomes current. */
export function WorkflowPage({ workflowID }: { workflowID: string }) {
  const workflow = useQuery(workflowQuery(workflowID));
  if (workflow.isError) {
    return (
      <div>
        <PageHeader title={s.places.workflows} />
        <p role="alert" className="text-sm text-danger">
          {s.workflows.editor.notFound}
        </p>
      </div>
    );
  }
  if (!workflow.data) return <p className="text-sm text-ink-muted">{s.workflows.loading}</p>;
  return <WorkflowEditor key={`${workflowID}-${workflow.data.version.number}`} existing={workflow.data} />;
}

/** A workflow being written: the YAML, what is wrong with it as you type, and saving it as a version. */
export function WorkflowEditor({ existing }: { existing?: WorkflowResponse }) {
  const id = useId();
  const client = useQueryClient();
  const navigate = useNavigate();
  const schema = useQuery(schemaQuery);
  const editor = useRef<YamlEditorHandle>(null);
  const [text, setText] = useState(existing?.version.specYAML ?? NEW_WORKFLOW);
  const [problems, setProblems] = useState<Problem[]>([]);
  const [checking, setChecking] = useState(false);
  const [comment, setComment] = useState("");
  const summary = existing?.workflow;
  const readOnly = summary ? !summary.canEdit || summary.archived : false;

  // The server checks what the schema cannot, against the tools as you see them; a pause in typing asks it.
  useEffect(() => {
    let cancelled = false;
    setChecking(true);
    const timer = setTimeout(async () => {
      try {
        const found = await validateWorkflow(text);
        if (!cancelled) setProblems(found);
      } finally {
        if (!cancelled) setChecking(false);
      }
    }, VALIDATE_DELAY_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [text]);

  const save = useMutation({
    mutationFn: () => (summary ? saveWorkflow(summary.id, text, comment, summary.currentVersion) : createWorkflow(text, comment)),
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: workflowsQueryKey });
      if (!summary) await navigate({ to: "/workflows/$workflowID", params: { workflowID: saved.workflow.id } });
    },
  });

  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (file) setText(await file.text());
    event.target.value = "";
  }

  function exportFile() {
    const url = URL.createObjectURL(new Blob([text], { type: "application/yaml" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = fileName(text);
    link.click();
    URL.revokeObjectURL(url);
  }

  const hintID = `${id}-hint`;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={summary?.name ?? s.workflows.editor.newTitle} />
      {summary && readOnly && (
        <p className="max-w-3xl rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-read-only>
          {summary.archived ? s.workflows.editor.archived : s.workflows.editor.readOnly}
        </p>
      )}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <section className="flex min-w-0 flex-col gap-3" aria-labelledby={`${id}-label`}>
          <h2 id={`${id}-label`} className="text-lg font-semibold text-ink">
            {s.workflows.editor.label}
          </h2>
          <p id={hintID} className="text-xs text-ink-muted">
            {s.workflows.editor.hint}
          </p>
          <YamlEditor
            value={text}
            onChange={setText}
            problems={problems}
            readOnly={readOnly}
            label={s.workflows.editor.label}
            describedBy={hintID}
            schema={schema.data}
            handle={editor}
          />
          <div aria-live="polite" data-problems>
            {checking ? (
              <p className="text-sm text-ink-muted">{s.workflows.editor.checking}</p>
            ) : problems.length === 0 ? (
              <p className="text-sm text-ink">{s.workflows.editor.valid}</p>
            ) : (
              <>
                <p className="text-sm font-medium text-danger">{s.workflows.editor.problems(problems.length)}</p>
                <ul className="mt-1 flex flex-col gap-1">
                  {problems.map((p) => (
                    <li key={`${p.path}-${p.code}-${p.line}-${p.column}`}>
                      <button
                        type="button"
                        className="text-left text-sm text-danger underline-offset-2 hover:underline"
                        onClick={() => editor.current?.focusAt(p.line, p.column)}
                        data-problem={p.code}
                      >
                        {p.line > 0 ? `${s.workflows.editor.at(p.line, p.column)}: ` : ""}
                        {p.message}
                      </button>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
          {!readOnly && (
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-sm font-medium text-ink">
                {s.workflows.editor.comment}
                <input className={input} value={comment} onChange={(e) => setComment(e.target.value)} maxLength={MAX_VERSION_COMMENT} name="comment" />
              </label>
              <div className="flex flex-wrap items-center gap-3">
                <button type="button" className={primary} disabled={save.isPending} onClick={() => save.mutate()} data-action="save-workflow">
                  {save.isPending ? s.workflows.editor.saving : summary ? s.workflows.editor.save : s.workflows.editor.create}
                </button>
                <label className={`${secondary} inline-flex cursor-pointer items-center`}>
                  {s.workflows.editor.import}
                  <input type="file" accept=".yaml,.yml,application/yaml,text/yaml" className="sr-only" onChange={importFile} data-import />
                </label>
                <button type="button" className={secondary} onClick={exportFile} data-action="export">
                  {s.workflows.editor.export}
                </button>
              </div>
            </div>
          )}
          {save.isSuccess && summary && (
            <p role="status" className="text-sm text-ink">
              {s.workflows.editor.saved(save.data.version.number)}
            </p>
          )}
          {save.isError && (
            <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-save-error>
              {save.error.message}
            </p>
          )}
        </section>
        <div className="flex flex-col gap-4">
          {!readOnly && <ToolPicker onInsert={(yaml) => editor.current?.insert(yaml)} />}
          {summary && <VersionsPanel workflowID={summary.id} current={summary.currentVersion} onOpen={setText} />}
          {summary?.canEdit && !summary.archived && <SharePanel workflow={summary} />}
        </div>
      </div>
    </div>
  );
}
