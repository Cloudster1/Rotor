import { describe, expect, it } from "vitest";
import { fieldsOf, inputsOf, valuesOf, WHOLE } from "./InputsForm";

const SCHEMA = {
  type: "object",
  properties: {
    title: { type: "string" },
    count: { type: "integer" },
    urgent: { type: "boolean" },
    tone: { type: "string", enum: ["plain", "formal"] },
    extra: { type: "object" },
  },
};

describe("valuesOf", () => {
  it("shows each kind of value as the form types it", () => {
    const fields = fieldsOf(SCHEMA);
    const values = valuesOf(fields, { title: "Billing", count: 3, urgent: false, tone: "formal", extra: { a: 1 } });
    expect(values).toEqual({ title: "Billing", count: "3", urgent: "false", tone: "formal", extra: JSON.stringify({ a: 1 }, null, 2) });
  });

  it("leaves out what is not set, so the field reads as empty", () => {
    expect(valuesOf(fieldsOf(SCHEMA), { title: null })).toEqual({});
    expect(valuesOf(fieldsOf(SCHEMA), undefined)).toEqual({});
  });

  it("turns back into the same inputs", () => {
    const fields = fieldsOf(SCHEMA);
    const given = { title: "Billing", count: 3, urgent: true, tone: "plain", extra: { a: [1, 2] } };
    expect(inputsOf(fields, valuesOf(fields, given)).inputs).toEqual(given);
  });

  it("shows a schema the form cannot lay out as one JSON document", () => {
    const values = valuesOf(null, { a: 1 });
    expect(JSON.parse(values[WHOLE] ?? "")).toEqual({ a: 1 });
  });
});
