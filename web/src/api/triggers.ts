import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A trigger as its owner sees it (docs/api-contract-m6.md). */
export type Trigger = components["schemas"]["TriggerResponse"];
export type TriggerRequest = components["schemas"]["TriggerRequest"];
export type DisabledReason = NonNullable<Trigger["disabled"]>["reason"];

export const triggersQueryKey = ["triggers"] as const;

/** The person's own triggers on every workflow, for the flags on the workflows list. */
export const myTriggersQuery = queryOptions({
  queryKey: [...triggersQueryKey, "mine"],
  queryFn: async (): Promise<Trigger[]> => {
    const { data } = await api.GET("/triggers");
    return (data as { triggers: Trigger[] }).triggers;
  },
});

/** The triggers on one workflow: the person's own, or everyone's for an administrator. */
export const workflowTriggersQuery = (workflowID: string) =>
  queryOptions({
    queryKey: [...triggersQueryKey, "workflow", workflowID],
    queryFn: async (): Promise<Trigger[]> => {
      const { data } = await api.GET("/workflows/{workflowID}/triggers", { params: { path: { workflowID } } });
      return (data as { triggers: Trigger[] }).triggers;
    },
  });

export async function createTrigger(workflowID: string, body: TriggerRequest): Promise<Trigger> {
  const { data } = await api.POST("/workflows/{workflowID}/triggers", { params: { path: { workflowID } }, body });
  return data as Trigger;
}

export async function changeTrigger(triggerID: string, body: TriggerRequest): Promise<Trigger> {
  const { data } = await api.PUT("/triggers/{triggerID}", { params: { path: { triggerID } }, body });
  return data as Trigger;
}

export async function setTriggerEnabled(triggerID: string, enabled: boolean): Promise<Trigger> {
  const params = { params: { path: { triggerID } } };
  const { data } = enabled ? await api.POST("/triggers/{triggerID}/enable", params) : await api.POST("/triggers/{triggerID}/disable", params);
  return data as Trigger;
}

export async function removeTrigger(triggerID: string): Promise<void> {
  await api.DELETE("/triggers/{triggerID}", { params: { path: { triggerID } } });
}

/** When a schedule fires next, in UTC; a schedule Rotor refuses throws with the field it is about. */
export async function previewSchedule(cron: string, timeZone: string): Promise<string[]> {
  const { data } = await api.POST("/triggers/schedule", { body: { cron, timeZone, noOverlap: false } });
  return (data as { nextRuns: string[] }).nextRuns;
}
