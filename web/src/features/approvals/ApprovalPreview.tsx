import type { PreviewWrite } from "@/api/approvals";
import { s } from "@/strings";

const pre = "overflow-x-auto whitespace-pre-wrap break-words rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink";

/** One write the run may make: a tool node's call with its arguments, or a node that picks its own calls. */
function Write({ write }: { write: PreviewWrite }) {
  const t = s.approvals.page;
  return (
    <li className="flex flex-col gap-1 rounded-control border border-border p-3" data-preview-write={write.node}>
      <p className="text-sm font-medium text-ink">
        {write.call ? t.call(write.node, write.call) : t.calls(write.node, write.calls?.length ? write.calls.join(", ") : t.anyCall)}
      </p>
      <p className="text-xs text-ink-muted">{t.through(write.tool, write.binding)}</p>
      {write.arguments === null || write.arguments === undefined ? (
        <p className="text-xs text-ink-muted">{t.ownArguments}</p>
      ) : (
        <pre className={pre}>{JSON.stringify(write.arguments, null, 2)}</pre>
      )}
    </li>
  );
}

/** What the run is about to write once approved, as the runner saw it when it stopped. */
export function ApprovalPreview({ writes }: { writes: PreviewWrite[] }) {
  return (
    <section aria-labelledby="approval-preview-title" className="flex flex-col gap-2" data-approval-preview>
      <h2 id="approval-preview-title" className="text-lg font-semibold text-ink">
        {s.approvals.page.preview}
      </h2>
      {writes.length === 0 ? (
        <p className="text-sm text-ink-muted">{s.approvals.page.previewNone}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {writes.map((write, i) => (
            <Write key={`${write.node}-${i}`} write={write} />
          ))}
        </ul>
      )}
    </section>
  );
}
