import { queryOptions } from "@tanstack/react-query";
import { META_STALE_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** What the server says it is: product, build, API version and capabilities. */
export type Meta = components["schemas"]["MetaResponse"];

export const metaQuery = queryOptions({
  queryKey: ["meta"],
  queryFn: async (): Promise<Meta> => {
    const { data } = await api.GET("/meta");
    return data as Meta;
  },
  staleTime: META_STALE_MS,
});
