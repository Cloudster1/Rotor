import { type ReactNode, useEffect, useId, useRef } from "react";

/** A modal over the canvas, as the run dialog is: it holds the focus, Escape closes it, and the focus goes back. */
export function CanvasDialog({
  title,
  onClose,
  children,
  ...rest
}: { title: string; onClose: () => void; children: ReactNode } & Record<`data-${string}`, string>) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (element && !element.open) element.showModal();
    return () => element?.close();
  }, []);
  return (
    <dialog
      ref={dialog}
      onClose={onClose}
      aria-labelledby={`${id}-title`}
      className="m-auto w-[min(36rem,calc(100vw-2rem))] rounded-overlay border border-border bg-surface-overlay p-0 text-ink shadow-2 backdrop:bg-black/40"
      {...rest}
    >
      <div className="flex max-h-[85vh] flex-col gap-4 overflow-y-auto p-5">
        <h2 id={`${id}-title`} className="text-lg font-semibold text-ink">
          {title}
        </h2>
        {children}
      </div>
    </dialog>
  );
}
