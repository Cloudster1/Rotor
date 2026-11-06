import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { setWorkspaces, type WorkspaceSetting, workspacesQuery, workspacesQueryKey } from "@/api/workspaces";
import { s } from "@/strings";
import { input, primary, secondary } from "./styles";
import { SettingsCard } from "./ui";

/** The toolchain images, package mirror and limits workspaces may use, for administrators (#136). */
export function WorkspacesSection() {
  const id = useId();
  const setting = useQuery(workspacesQuery);
  return (
    <SettingsCard titleId={`${id}-title`} title={s.workspaces.title} description={s.workspaces.body} wide data-workspaces>
      {setting.isPending && <p className="text-sm text-ink-muted">{s.workspaces.loading}</p>}
      {setting.isError && (
        <p role="alert" className="text-sm text-danger">
          {setting.error.message}
        </p>
      )}
      {setting.data && <WorkspacesForm current={setting.data} labelledBy={`${id}-title`} />}
    </SettingsCard>
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
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <input
                  className={`${input} sm:max-w-48`}
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
                <button
                  type="button"
                  className={`${secondary} self-start sm:self-auto`}
                  onClick={() => setForm({ ...form, images: form.images.filter((_, j) => j !== i) })}
                >
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
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {field("packageMirror", s.workspaces.packageMirrorHint)}
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
