import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { accessQueryKey, createRole, type Role, updateRole } from "@/api/access";
import { NEW_ROLE_PERMISSIONS } from "@/config";
import { input, primary, secondary } from "@/features/settings/styles";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";
import { Dialog, label, Refusal } from "./ui";

const menuItem = "flex h-8 w-full items-center rounded-control px-2 text-left text-sm text-ink hover:bg-surface-raised disabled:opacity-60";

/** A role's own actions behind one button, so the matrix's heads stay narrow; Escape or a click elsewhere closes it. */
export function RoleMenu({ role, onRename, onDelete }: { role: Role; onRename: () => void; onDelete: () => void }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (event: MouseEvent) => {
      if (!box.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);
  const choose = (action: () => void) => {
    setOpen(false);
    action();
  };
  return (
    <div
      ref={box}
      className="relative"
      onKeyDown={(event) => {
        if (event.key === "Escape" && open) {
          event.stopPropagation();
          setOpen(false);
          button.current?.focus();
        }
      }}
    >
      <button
        ref={button}
        type="button"
        className="flex size-6 items-center justify-center rounded-control text-ink-muted hover:bg-surface-raised hover:text-ink"
        aria-label={s.access.matrix.actionsFor(role.name)}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
        data-action="role-menu"
      >
        <Icon.More />
      </button>
      {open && (
        <div
          id={id}
          className="absolute right-0 z-10 mt-1 flex w-48 flex-col gap-0.5 rounded-control border border-border bg-surface-overlay p-1 font-normal shadow-2"
        >
          <button type="button" className={menuItem} onClick={() => choose(onRename)} data-action="rename-role">
            {s.access.matrix.rename}
          </button>
          <button type="button" className={`${menuItem} text-danger`} disabled={role.builtin} onClick={() => choose(onDelete)} data-action="delete-role">
            {s.access.delete}
          </button>
        </div>
      )}
    </div>
  );
}

function NameFields({
  name,
  setName,
  description,
  setDescription,
}: {
  name: string;
  setName: (v: string) => void;
  description: string;
  setDescription: (v: string) => void;
}) {
  return (
    <>
      <label className={label}>
        {s.access.matrix.name}
        {/* The first field of a modal takes the focus when it opens, so the name needs no autoFocus. */}
        <input className={input} value={name} onChange={(event) => setName(event.target.value)} data-role-name="" />
      </label>
      <label className={label}>
        {s.access.matrix.purpose}
        <input className={input} value={description} onChange={(event) => setDescription(event.target.value)} data-role-description="" />
      </label>
    </>
  );
}

function Buttons({ submit, pending, disabled, onClose }: { submit: string; pending: boolean; disabled: boolean; onClose: () => void }) {
  return (
    <div className="flex flex-wrap justify-end gap-2">
      <button type="button" className={secondary} onClick={onClose}>
        {s.access.cancel}
      </button>
      <button type="submit" className={primary} disabled={pending || disabled} data-action="save-role">
        {submit}
      </button>
    </div>
  );
}

/** Adds a role, starting with seeing workflows; the rest is ticked in the matrix. */
export function NewRoleDialog({ onClose, onAdded }: { onClose: () => void; onAdded: (role: Role) => void }) {
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [installationWideOnly, setInstallationWideOnly] = useState(false);
  const create = useMutation({
    mutationFn: () => createRole({ name: name.trim(), description: description.trim(), installationWideOnly, permissions: NEW_ROLE_PERMISSIONS }),
    onSuccess: async (role) => {
      await client.invalidateQueries({ queryKey: accessQueryKey });
      onAdded(role);
    },
  });
  return (
    <Dialog title={s.access.matrix.newRole} onClose={onClose} onSubmit={() => name.trim() && create.mutate()} data-new-role="">
      <NameFields name={name} setName={setName} description={description} setDescription={setDescription} />
      <label className="flex items-center gap-2 text-sm text-ink-muted">
        <input
          type="checkbox"
          className="size-4 accent-accent"
          checked={installationWideOnly}
          onChange={(event) => setInstallationWideOnly(event.target.checked)}
          data-role-installation-wide=""
        />
        {s.access.matrix.allAtOnceDetail}
      </label>
      <p className="text-xs text-ink-muted">{s.access.matrix.startsWith}</p>
      <Refusal error={create.error} />
      <Buttons submit={s.access.matrix.addRole} pending={create.isPending} disabled={!name.trim()} onClose={onClose} />
    </Dialog>
  );
}

/** Renames a role or says what it is for; built-in ones too, as in Armature. */
export function RenameRoleDialog({ role, onClose, onSaved }: { role: Role; onClose: () => void; onSaved: (name: string) => void }) {
  const client = useQueryClient();
  const [name, setName] = useState(role.name);
  const [description, setDescription] = useState(role.description);
  const update = useMutation({
    mutationFn: () => updateRole(role.key, { name: name.trim(), description: description.trim() }),
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: accessQueryKey });
      onSaved(saved.name);
    },
  });
  return (
    <Dialog title={s.access.matrix.renameTitle(role.name)} onClose={onClose} onSubmit={() => name.trim() && update.mutate()} data-rename-role={role.key}>
      <NameFields name={name} setName={setName} description={description} setDescription={setDescription} />
      <Refusal error={update.error} />
      <Buttons submit={s.access.save} pending={update.isPending} disabled={!name.trim()} onClose={onClose} />
    </Dialog>
  );
}
