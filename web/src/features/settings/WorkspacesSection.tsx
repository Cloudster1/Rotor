import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { setWorkspaces, type WorkspaceSetting, workspacesQuery, workspacesQueryKey } from "@/api/workspaces";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm text-ink hover:bg-surface-raised";

/** The toolchain images, package mirror and limits workspaces may use, for administrators (#136). */
export function WorkspacesSection() {
  const id = useId();
  const setting = useQuery(workspacesQuery);
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-workspaces>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {s.workspaces.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.workspaces.body}</p>
      {setting.isPending && <p className="text-sm text-ink-muted">{s.workspaces.loading}</p>}
      {setting.isError && (
        <p role="alert" className="text-sm text-danger">
          {setting.error.message}
        </p>
      )}
      {setting.data && <WorkspacesForm current={setting.data} labelledBy={`${id}-title`} />}
    </section>
  );
}

function WorkspacesForm({ current, labelledBy }: { current: WorkspaceSetting; labelledBy: string }) {
  const id = useId();
  const client = useQueryClient();
  const [form, setForm] = useState<WorkspaceSetting>(current);
  const save = useMutation({
    mutationFn: () => setWorkspaces(form),
    onSuccess: (next) => {
      client.setQueryData(workspacesQueryKey, next);
      setForm(next);
    },
  });
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const setImage = (i: number, key: "name" | "reference", value: string) =>
    setForm({ ...form, images: form.images.map((image, j) => (i === j ? { ...image, [key]: value } : image)) });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  const field = (name: "packageMirror" | "maxStepTime" | "maxVolume", hint: string) => (
    <div className="flex flex-col gap-1">
      <label htmlFor={`${id}-${name}`} className="text-sm font-medium text-ink">
        {s.workspaces[name]}
      </label>
      <input
        id={`${id}-${name}`}
        className={input}
        value={form[name]}
        onChange={(e) => setForm({ ...form, [name]: e.target.value })}
        aria-describedby={`${id}-${name}-hint`}
        aria-invalid={fields[name] ? true : undefined}
        data-workspaces-field={name}
      />
      <p id={`${id}-${name}-hint`} className={`text-xs ${fields[name] ? "text-danger" : "text-ink-muted"}`}>
        {fields[name] ?? hint}
      </p>
    </div>
  );
  return (
    <form className="flex flex-col gap-4" onSubmit={submit} noValidate aria-labelledby={labelledBy}>
      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-medium text-ink">{s.workspaces.images}</legend>
        <p className="text-xs text-ink-muted">{s.workspaces.imagesHint}</p>
        {form.images.length === 0 && <p className="text-sm text-ink">{s.workspaces.noImages}</p>}
        {form.images.map((image, i) => {
          const problem = fields[`images/${i}/name`] ?? fields[`images/${i}/reference`];
          return (
            // biome-ignore lint/suspicious/noArrayIndexKey: an image's place is all a row has before it is named
            <div key={i} className="flex flex-col gap-1" data-workspaces-image={i}>
              <div className="flex gap-2">
                <input
                  className={`${input} w-40`}
                  aria-label={s.workspaces.imageName(i + 1)}
                  value={image.name}
                  onChange={(e) => setImage(i, "name", e.target.value)}
                />
                <input
                  className={input}
                  aria-label={s.workspaces.imageReference(i + 1)}
                  value={image.reference}
                  onChange={(e) => setImage(i, "reference", e.target.value)}
                />
                <button type="button" className={secondary} onClick={() => setForm({ ...form, images: form.images.filter((_, j) => j !== i) })}>
                  {s.workspaces.removeImage(image.name)}
                </button>
              </div>
              {problem && <p className="text-xs text-danger">{problem}</p>}
            </div>
          );
        })}
        <div>
          <button type="button" className={secondary} onClick={() => setForm({ ...form, images: [...form.images, { name: "", reference: "" }] })}>
            {s.workspaces.addImage}
          </button>
        </div>
      </fieldset>
      {field("packageMirror", s.workspaces.packageMirrorHint)}
      <div className="grid grid-cols-2 gap-3">
        {field("maxStepTime", s.workspaces.maxStepTimeHint)}
        {field("maxVolume", s.workspaces.maxVolumeHint)}
      </div>
      {save.isError && (
        <p role="alert" className="text-sm text-danger" data-workspaces-error>
          {save.error.message}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink">
          {s.workspaces.saved}
        </p>
      )}
      <div>
        <button type="submit" className={primary} disabled={save.isPending}>
          {save.isPending ? s.workspaces.saving : s.workspaces.save}
        </button>
      </div>
    </form>
  );
}
