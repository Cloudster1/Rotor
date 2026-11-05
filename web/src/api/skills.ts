import { queryOptions } from "@tanstack/react-query";
import { API_BASE, SKILL_PAGE_SIZE, SKILL_PICKER_LIMIT } from "@/config";
import type { Assignment, GrantInput } from "./access";
import { api } from "./client";
import type { components } from "./schema";

/** A skill in a list and with its current version, as the library answers them (#219). */
export type SkillSummary = components["schemas"]["SkillSummary"];
export type SkillResponse = components["schemas"]["SkillResponse"];
export type SkillVersion = components["schemas"]["SkillVersionResponse"];
export type SkillVersionSummary = components["schemas"]["SkillVersionSummary"];
export type SkillList = components["schemas"]["SkillList"];
export type SkillDiff = components["schemas"]["SkillDiffResponse"];
export type SkillFileBody = components["schemas"]["SkillFileBody"];
export type ImportedSkill = components["schemas"]["ImportedSkill"];
export type SkillProblem = components["schemas"]["Problem"];

export const skillsQueryKey = ["skills"] as const;

/** What the list asks for: archived or not, a search, and where the page starts. */
export interface SkillFilter {
  archived: boolean;
  q: string;
  offset: number;
}

export const skillsQuery = ({ archived, q, offset }: SkillFilter, limit: number = SKILL_PAGE_SIZE) =>
  queryOptions({
    queryKey: [...skillsQueryKey, "list", { archived, q, offset, limit }],
    queryFn: async (): Promise<SkillList> => {
      const { data } = await api.GET("/skills", {
        params: { query: { archived: archived ? "true" : undefined, q: q || undefined, limit, offset: offset || undefined } },
      });
      return data as SkillList;
    },
    placeholderData: (previous) => previous,
  });

/** The skills the workflow editor's picker offers: every active one the person reads, up to the server's page limit. */
export const pickableSkillsQuery = skillsQuery({ archived: false, q: "", offset: 0 }, SKILL_PICKER_LIMIT);

export const skillQuery = (id: string) =>
  queryOptions({
    queryKey: [...skillsQueryKey, id],
    queryFn: async (): Promise<SkillResponse> => {
      const { data } = await api.GET("/skills/{skillID}", { params: { path: { skillID: id } } });
      return data as SkillResponse;
    },
  });

export const skillVersionsQuery = (id: string) =>
  queryOptions({
    queryKey: [...skillsQueryKey, id, "versions"],
    queryFn: async (): Promise<SkillVersionSummary[]> => {
      const { data } = await api.GET("/skills/{skillID}/versions", { params: { path: { skillID: id } } });
      return (data?.versions ?? []) as SkillVersionSummary[];
    },
  });

// A version and its files never change once saved, so what was read once stays true.
export const skillVersionQuery = (id: string, number: number) =>
  queryOptions({
    queryKey: [...skillsQueryKey, id, "versions", number],
    queryFn: async (): Promise<SkillVersion> => {
      const { data } = await api.GET("/skills/{skillID}/versions/{versionNumber}", { params: { path: { skillID: id, versionNumber: number } } });
      return data as SkillVersion;
    },
    staleTime: Number.POSITIVE_INFINITY,
  });

export const skillFileQuery = (id: string, number: number, path: string) =>
  queryOptions({
    queryKey: [...skillsQueryKey, id, "versions", number, "file", path],
    queryFn: async (): Promise<string> => {
      const { data } = await api.GET("/skills/{skillID}/versions/{versionNumber}/file", {
        params: { path: { skillID: id, versionNumber: number }, query: { path } },
      });
      return data?.content ?? "";
    },
    staleTime: Number.POSITIVE_INFINITY,
  });

export const skillDiffQuery = (id: string, from: number, to: number) =>
  queryOptions({
    queryKey: [...skillsQueryKey, id, "diff", from, to],
    queryFn: async (): Promise<SkillDiff> => {
      const { data } = await api.GET("/skills/{skillID}/diff", { params: { path: { skillID: id }, query: { from: String(from), to: String(to) } } });
      return data as SkillDiff;
    },
    staleTime: Number.POSITIVE_INFINITY,
  });

export const skillGrantsQuery = (id: string) =>
  queryOptions({
    queryKey: ["access", "skill", id],
    queryFn: async (): Promise<Assignment[]> => {
      const { data } = await api.GET("/skills/{skillID}/grants", { params: { path: { skillID: id } } });
      return (data?.grants ?? []) as Assignment[];
    },
  });

/** Checks files as a save would, without saving; the name's being taken is the save's to say. */
export async function validateSkill(files: SkillFileBody[]): Promise<SkillProblem[]> {
  const { data } = await api.POST("/skills/validate", { body: { files } });
  return (data?.problems ?? []) as SkillProblem[];
}

export async function createSkill(files: SkillFileBody[], comment: string): Promise<SkillResponse> {
  const { data } = await api.POST("/skills", { body: { files, comment } });
  return data as SkillResponse;
}

export async function saveSkill(id: string, files: SkillFileBody[], comment: string, baseVersion: number): Promise<SkillResponse> {
  const { data } = await api.PUT("/skills/{skillID}", { params: { path: { skillID: id } }, body: { files, comment, baseVersion } });
  return data as SkillResponse;
}

export async function archiveSkill(id: string): Promise<void> {
  await api.DELETE("/skills/{skillID}", { params: { path: { skillID: id } } });
}

export async function restoreSkill(id: string): Promise<void> {
  await api.POST("/skills/{skillID}/restore", { params: { path: { skillID: id } } });
}

/** Sends a zip as it is; the answer says whether a skill was made or a version added to the one of that name. */
export async function importSkill(zip: Blob, comment: string): Promise<ImportedSkill> {
  const { data } = await api.POST("/skills/import", {
    params: { query: { comment: comment || undefined } },
    body: zip as unknown as string,
    bodySerializer: (body) => body as unknown as Blob,
    headers: { "Content-Type": "application/zip" },
  });
  return data as ImportedSkill;
}

/** Where a version downloads as a zip; the browser sends the session's cookie with it. */
export function exportURL(id: string, number: number): string {
  return `${API_BASE}/skills/${encodeURIComponent(id)}/versions/${number}/export`;
}

export async function grantOnSkill(id: string, body: Omit<GrantInput, "workflowId">): Promise<Assignment> {
  const { data } = await api.POST("/skills/{skillID}/grants", { params: { path: { skillID: id } }, body });
  return data?.grant as Assignment;
}

export async function revokeOnSkill(id: string, grantID: string): Promise<void> {
  await api.DELETE("/skills/{skillID}/grants/{grantID}", { params: { path: { skillID: id, grantID } } });
}
