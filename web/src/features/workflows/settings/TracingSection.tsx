import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { setWorkflowTracing, type WorkflowTracingResponse, workflowTracingQuery, workflowTracingQueryKey } from "@/api/workflowTracing";
import { input, primary } from "@/features/settings/styles";
import { SettingsCard, SettingsPage } from "@/features/settings/ui";
import { Refusal } from "@/features/shell/Dialog";
import { s } from "@/strings";
import { SectionNote, useSettingsWorkflow } from "./shared";

// Fine enough to pick a share by the arrows; the server takes any number in range.
const RATIO_STEP = 0.05;

const legend = "text-sm font-medium text-ink";
const choice = "flex items-start gap-2 text-sm text-ink";
const hint = "text-xs text-ink-muted";

/** How the workflow's runs are traced: whether their text is kept, where they go and how many are sent (#259). */
export function TracingPage() {
  const { workflowID } = useSettingsWorkflow();
  const tracing = useQuery(workflowTracingQuery(workflowID));
  return (
    <SettingsPage title={s.workflowSettings.pages.tracing}>
      {tracing.isPending && <p className="text-sm text-ink-muted lg:col-span-full">{s.workflowSettings.tracing.loading}</p>}
      {tracing.isError && (
        <p role="alert" className="text-sm text-danger lg:col-span-full">
          {tracing.error.message}
        </p>
      )}
      {tracing.data && <TracingForm workflowID={workflowID} current={tracing.data} />}
    </SettingsPage>
  );
}

function ratioOf(text: string): number | null {
  if (text.trim() === "") return null;
  const value = Number(text);
  return Number.isFinite(value) && value >= 0 && value <= 1 ? value : null;
}

function TracingForm({ workflowID, current }: { workflowID: string; current: WorkflowTracingResponse }) {
  const id = useId();
  const client = useQueryClient();
  const t = s.workflowSettings.tracing;
  const { tracing, installation, destinations, canChange } = current;
  const [content, setContent] = useState(tracing.content);
  const [chosen, setChosen] = useState<string[] | null>(tracing.destinations);
  const [ratio, setRatio] = useState(String(tracing.sampleRatio));
  const [problem, setProblem] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (sampleRatio: number) => setWorkflowTracing(workflowID, { content, destinations: chosen, sampleRatio }),
    onSuccess: (next) => client.setQueryData(workflowTracingQueryKey(workflowID), next),
  });
  const withText = destinations.filter((each) => each.content).map((each) => each.name);

  function submit(event: FormEvent) {
    event.preventDefault();
    setProblem(null);
    const value = ratioOf(ratio);
    if (value === null) return setProblem(t.shareProblem);
    save.mutate(value);
  }

  function toggle(destinationID: string, on: boolean) {
    const held = chosen ?? [];
    setChosen(on ? [...held, destinationID] : held.filter((each) => each !== destinationID));
  }

  return (
    <form className="contents" noValidate onSubmit={submit} data-tracing-form="">
      {!canChange && <SectionNote data-read-only="tracing">{t.readOnly}</SectionNote>}
      <SettingsCard titleId={`${id}-installation`} title={t.installationTitle} data-tracing-installation={installation.keepContent ? "keeps" : "none"}>
        <p className="text-sm text-ink">{installation.keepContent ? t.installationKeeps : t.installationKeepsNone}</p>
        <p className={`mt-1 ${hint}`}>{withText.length > 0 ? t.destinationsWithText(withText) : t.destinationsWithoutText}</p>
      </SettingsCard>

      <SettingsCard titleId={`${id}-text`} title={t.textTitle} description={t.textBody} data-tracing-text="">
        <fieldset className="flex flex-col gap-3" disabled={!canChange}>
          <legend className={`mb-2 ${legend}`}>{t.textLegend}</legend>
          <label className={choice}>
            <input type="radio" name="content" value="inherit" checked={content === "inherit"} onChange={() => setContent("inherit")} />
            <span>
              {t.inherit}
              <span className={`block ${hint}`}>{t.inheritHint}</span>
            </span>
          </label>
          <label className={choice}>
            <input type="radio" name="content" value="none" checked={content === "none"} onChange={() => setContent("none")} />
            <span>
              {t.none}
              <span className={`block ${hint}`}>{t.noneHint}</span>
            </span>
          </label>
        </fieldset>
      </SettingsCard>

      <SettingsCard titleId={`${id}-destinations`} title={t.destinationsTitle} description={t.destinationsBody} data-tracing-destinations="">
        {destinations.length === 0 ? (
          <p className="text-sm text-ink" data-no-destinations="">
            {t.noDestinations}
          </p>
        ) : (
          <fieldset className="flex flex-col gap-3" disabled={!canChange}>
            <legend className={`mb-2 ${legend}`}>{t.destinationsLegend}</legend>
            <label className={choice}>
              <input type="radio" name="scope" value="all" checked={chosen === null} onChange={() => setChosen(null)} />
              {t.everyDestination}
            </label>
            <label className={choice}>
              <input type="radio" name="scope" value="chosen" checked={chosen !== null} onChange={() => setChosen(chosen ?? [])} />
              {t.chosenDestinations}
            </label>
            {chosen !== null && (
              <fieldset className="ml-6 flex flex-col gap-2" data-chosen-destinations="" disabled={!canChange}>
                <legend className="sr-only">{t.chosenLegend}</legend>
                {destinations.map((each) => (
                  <label key={each.id} className={choice} data-destination={each.name}>
                    <input type="checkbox" checked={chosen.includes(each.id)} onChange={(e) => toggle(each.id, e.target.checked)} />
                    <span>
                      {each.name}
                      {!each.enabled && ` (${t.disabled})`}
                      {each.content && ` (${t.takesText})`}
                    </span>
                  </label>
                ))}
              </fieldset>
            )}
          </fieldset>
        )}
      </SettingsCard>

      <SettingsCard titleId={`${id}-share`} title={t.shareTitle} description={t.shareBody} data-tracing-share="">
        <label className="flex max-w-40 flex-col gap-1 text-sm font-medium text-ink">
          {t.shareLabel}
          <input
            className={input}
            name="sampleRatio"
            type="number"
            min={0}
            max={1}
            step={RATIO_STEP}
            value={ratio}
            onChange={(e) => setRatio(e.target.value)}
            disabled={!canChange}
            aria-describedby={`${id}-share-hint`}
          />
        </label>
        <p id={`${id}-share-hint`} className={`mt-1 ${hint}`}>
          {t.shareHint}
        </p>
      </SettingsCard>

      <div className="flex flex-col gap-3 lg:col-span-full">
        {tracing.changedBy && <p className={hint}>{t.changedBy(tracing.changedBy)}</p>}
        {canChange && (
          <div>
            <button type="submit" className={primary} disabled={save.isPending} data-action="save-tracing">
              {save.isPending ? t.saving : t.save}
            </button>
          </div>
        )}
        {problem && (
          <p role="alert" className="text-sm text-danger">
            {problem}
          </p>
        )}
        <Refusal error={save.error} />
        <div role="status" className="text-sm text-ink">
          {save.isSuccess && t.saved}
        </div>
      </div>
    </form>
  );
}
