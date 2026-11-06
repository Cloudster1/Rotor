import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { workflowQuery } from "@/api/workflows";
import { workflowSettingsGroups } from "@/features/settings/groups";
import { Layout } from "@/features/settings/SettingsLayout";
import { link } from "@/features/settings/styles";
import { SettingsPage } from "@/features/settings/ui";
import { PageHeader } from "@/features/shell/EmptyState";
import { TriggersSection } from "@/features/triggers/TriggersSection";
import { s } from "@/strings";
import { VersionsPanel } from "../VersionsPanel";
import { WorkflowGrantsPanel } from "../WorkflowGrantsPanel";
import { SectionNote, useSettingsWorkflow } from "./shared";

export { GeneralPage } from "./GeneralSection";
export { TracingPage } from "./TracingSection";

/** A workflow's settings: whose they are, a way back to its editor, and its sections beside the one open. */
export function WorkflowSettingsLayout() {
  const { workflowID } = useParams({ from: "/shell/workflows/$workflowID/settings" });
  const workflow = useQuery(workflowQuery(workflowID));
  if (workflow.isError) {
    return (
      <div>
        <PageHeader title={s.places.workflows} />
        <p role="alert" className="text-sm text-danger" data-settings-not-found>
          {s.workflowSettings.notFound}
        </p>
      </div>
    );
  }
  if (!workflow.data) return <p className="text-sm text-ink-muted">{s.workflowSettings.loading}</p>;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-sm font-medium text-ink-muted" data-settings-of={workflow.data.workflow.name}>
          {s.workflowSettings.of(workflow.data.workflow.name)}
        </p>
        <Link to="/workflows/$workflowID" params={{ workflowID }} className={link} data-action="open-editor">
          {s.workflowSettings.editor}
        </Link>
      </div>
      <Layout label={s.workflowSettings.nav} groups={workflowSettingsGroups(workflowID)} />
    </div>
  );
}

/** Every version, newest first, and what changed between two; an older one opens in the editor to be saved again. */
export function VersionsPage() {
  const { workflowID, existing } = useSettingsWorkflow();
  const navigate = useNavigate();
  if (!existing) return null;
  return (
    <SettingsPage title={s.workflowSettings.pages.versions}>
      <SectionNote data-versions-note="">{s.workflowSettings.versionsReadOnly}</SectionNote>
      <div className="lg:col-span-full">
        <VersionsPanel
          workflowID={workflowID}
          current={existing.workflow.currentVersion}
          onOpen={(version) => navigate({ to: "/workflows/$workflowID", params: { workflowID }, search: { version } })}
        />
      </div>
    </SettingsPage>
  );
}

/** Who holds a role on the workflow; its administrators change it, anybody else reads it. */
export function SharingPage() {
  const { existing } = useSettingsWorkflow();
  if (!existing) return null;
  return (
    <SettingsPage title={s.workflowSettings.pages.sharing}>
      <SectionNote data-skills-note="">{s.skills.workflowNote}</SectionNote>
      <div className="lg:col-span-full">
        <WorkflowGrantsPanel workflow={existing.workflow} />
      </div>
    </SettingsPage>
  );
}

/** The triggers that start the workflow: a runner adds their own, an administrator of it turns off and removes anybody's. */
export function TriggersPage() {
  const { workflowID, existing } = useSettingsWorkflow();
  if (!existing) return null;
  const { workflow, version } = existing;
  const add = workflow.canRun && !workflow.archived;
  return (
    <SettingsPage title={s.workflowSettings.pages.triggers}>
      {!add && (
        <SectionNote data-read-only="triggers">{workflow.archived ? s.workflowSettings.triggersArchived : s.workflowSettings.triggersReadOnly}</SectionNote>
      )}
      <div className="lg:col-span-full">
        <TriggersSection
          workflowID={workflowID}
          inputsSchema={version.inputs}
          unattendedCalls={version.unattendedCalls}
          rights={{ add, administer: workflow.canAdminister }}
        />
      </div>
    </SettingsPage>
  );
}
