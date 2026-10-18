import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// WCAG AA for body text; a token used only for large text or icons may sit at AA_LARGE_TEXT_OR_ICON.
const AA_TEXT = 4.5;
const AA_LARGE_TEXT_OR_ICON = 3;

const TEXT_TOKENS = ["ink", "ink-muted", "ink-subtle", "ink-disabled"];
const SURFACE_TOKENS = ["canvas", "surface", "surface-raised", "surface-sunken", "surface-overlay"];

// Every text token is held to AA_TEXT except the ones named here, each with the reason it may sit lower.
const NO_BAR = 1;
const LOWER_BAR: Record<string, { ratio: number; reason: string }> = {
  "ink-disabled": {
    ratio: NO_BAR,
    reason: "WCAG 1.4.3 exempts the text of inactive controls, and the faded ink is what tells a person the control is off",
  },
};

type Palette = Record<string, string>;

function channel(hex: string, offset: number): number {
  const value = parseInt(hex.slice(offset, offset + 2), 16) / 255;
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  return 0.2126 * channel(hex, 1) + 0.7152 * channel(hex, 3) + 0.0722 * channel(hex, 5);
}

/** The WCAG contrast ratio of two #rrggbb colours. */
function contrast(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light! + 0.05) / (dark! + 0.05);
}

function declarations(block: string): Palette {
  return Object.fromEntries([...block.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1]!, m[2]!.trim().toLowerCase()]));
}

const css = readFileSync(join(__dirname, "tokens.css"), "utf8");
const clayLight = declarations(css.match(/@theme \{([\s\S]*?)\n\}/)![1]!);
const clayDarkChoice = declarations(css.match(/\n {2}:root\[data-theme="dark"\] \{([\s\S]*?)\n {2}\}/)![1]!);
const clayDarkSystem = declarations(css.match(/@media \(prefers-color-scheme: dark\) \{([\s\S]*?)\n {4}\}/)![1]!);

const cases = [
  { name: "Clay light", palette: clayLight },
  { name: "Clay dark", palette: { ...clayLight, ...clayDarkChoice } },
];

describe("text contrast", () => {
  it("reads both dark blocks, which say the same", () => {
    expect(clayDarkSystem).toEqual(clayDarkChoice);
    for (const { palette } of cases) {
      for (const token of [...TEXT_TOKENS, ...SURFACE_TOKENS]) expect(palette[token], token).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  it("lowers the bar only for text tokens, each with a reason", () => {
    for (const [token, { ratio, reason }] of Object.entries(LOWER_BAR)) {
      expect(TEXT_TOKENS).toContain(token);
      expect([AA_LARGE_TEXT_OR_ICON, NO_BAR]).toContain(ratio);
      expect(reason).not.toBe("");
    }
  });

  describe.each(cases)("$name", ({ palette }) => {
    const pairs = TEXT_TOKENS.flatMap((text) => SURFACE_TOKENS.map((surface) => ({ text, surface })));
    it.each(pairs)("$text on $surface", ({ text, surface }) => {
      const needed = LOWER_BAR[text]?.ratio ?? AA_TEXT;
      const ratio = contrast(palette[text]!, palette[surface]!);
      expect(ratio, `${text} ${palette[text]} on ${surface} ${palette[surface]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(needed);
    });
  });
});

// Links and the current item in the accent, errors in danger, each on any surface and on its own tint.
const COLOURED_TEXT = [
  { text: "accent", tint: "accent-subtle" },
  { text: "danger", tint: "danger-subtle" },
];

// The fills a label is written on, with the token that writes it, hover included.
const FILLS = [
  { text: "on-primary", fill: "primary" },
  { text: "on-primary", fill: "primary-hover" },
  { text: "on-accent", fill: "accent" },
  { text: "on-accent", fill: "accent-hover" },
  { text: "on-danger", fill: "danger" },
  { text: "on-danger", fill: "danger-hover" },
];

describe("coloured text and labels on fills", () => {
  describe.each(cases)("$name", ({ palette }) => {
    const pairs = [
      ...COLOURED_TEXT.flatMap(({ text, tint }) => [...SURFACE_TOKENS, tint].map((background) => ({ text, background }))),
      ...FILLS.map(({ text, fill }) => ({ text, background: fill })),
    ];
    it.each(pairs)("$text on $background", ({ text, background }) => {
      const ratio = contrast(palette[text]!, palette[background]!);
      expect(ratio, `${text} ${palette[text]} on ${background} ${palette[background]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(AA_TEXT);
    });
  });
});
