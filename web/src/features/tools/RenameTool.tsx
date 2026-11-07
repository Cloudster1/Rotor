import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { connectionsQueryKey } from "@/api/connections";
import { renameTool, type Tool, type ToolBinder, toolWorkflowsQuery, toolsQueryKey } from "@/api/tools";
import { danger, input, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function Binders({ binders }: { binders: ToolBinder[] }) {
  return (
    <ul className="list-disc pl-5">
      {binders.map((b) => (
        <li key={b.id} data-binder={b.name}>
          <a className="text-accent underline-offset-2 hover:underline" href={`/workflows/${b.id}`}>
            {b.name}
          </a>{" "}
          <span className="text-ink-muted">({s.toolNames.owner(b.ownerName)})</span>
        </li>
      ))}
    </ul>
  );
}

/** Renaming a tool (#267): the workflows binding its name are listed first, since they stop finding it. */
export function RenameTool({ tool }: { tool: Tool }) {
  const id = useId();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(tool.name);
  const binders = useQuery({ ...toolWorkflowsQuery(tool.id), enabled: open });
  const rename = useMutation({
    mutationFn: async () => ({ from: tool.name, ...(await renameTool(tool.id, name.trim())) }),
    onSuccess: () => {
      setOpen(false);
      // Tool accounts names each tool too, so both lists are read again.
      return Promise.all([client.invalidateQueries({ queryKey: toolsQueryKey }), client.invalidateQueries({ queryKey: connectionsQueryKey })]);
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    rename.mutate();
  }
  if (!open) {
    const done = rename.data;
    return (
      <>
        <button
          type="button"
          className={secondary}
          onClick={() => {
            setName(tool.name);
            rename.reset();
            setOpen(true);
          }}
          data-action="rename-tool"
        >
          {s.toolNames.rename}
        </button>
        {done && (
          <div role="status" className="basis-full text-sm text-ink" data-renamed>
            <p className="text-success">{s.toolNames.renamed(done.from, done.tool.name)}</p>
            {done.workflows.length > 0 && (
              <>
                <p>{s.toolNames.stillBound(done.from)}</p>
                <Binders binders={done.workflows} />
              </>
            )}
          </div>
        )}
      </>
    );
  }
  return (
    <form
      onSubmit={submit}
      className="flex basis-full flex-col gap-2 rounded-control border border-border p-3"
      aria-labelledby={`${id}-label`}
      data-rename-tool
    >
      <label id={`${id}-label`} htmlFor={`${id}-name`} className="text-sm font-medium text-ink">
        {s.toolNames.renameLabel(tool.name)}
      </label>
      <input
        id={`${id}-name`}
        name="name"
        required
        className={input}
        value={name}
        onChange={(e) => setName(e.target.value)}
        aria-describedby={`${id}-hint ${id}-binders`}
      />
      <p id={`${id}-hint`} className="text-xs text-ink-muted">
        {s.toolNames.renameHint}
      </p>
      <div id={`${id}-binders`} className="text-sm text-ink" data-binders>
        {binders.isPending && <p className="text-ink-muted">{s.toolNames.loadingBinders}</p>}
        {binders.isError && <p className="text-danger">{message(binders.error)}</p>}
        {binders.data && binders.data.length === 0 && <p className="text-ink-muted">{s.toolNames.noBinders(tool.name)}</p>}
        {binders.data && binders.data.length > 0 && (
          <div className="rounded-control border border-warning bg-warning-subtle px-3 py-2">
            <p>{s.toolNames.binders(tool.name)}</p>
            <Binders binders={binders.data} />
          </div>
        )}
      </div>
      {rename.isError && (
        <p role="alert" className="text-sm text-danger" data-rename-error>
          {message(rename.error)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <button type="submit" className={danger} disabled={rename.isPending || name.trim() === ""} data-action="confirm-rename">
          {rename.isPending ? s.toolNames.saving : s.toolNames.save}
        </button>
        <button type="button" className={secondary} onClick={() => setOpen(false)}>
          {s.toolNames.keep}
        </button>
      </div>
    </form>
  );
}
