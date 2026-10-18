import type { SVGProps } from "react";
import { ICON_SIZE_PX, ICON_STROKE } from "@/config";

type IconProps = SVGProps<SVGSVGElement>;

// Every glyph is a 24-unit line drawing scaled to the icon grid; decorative unless a label is given.
function glyph(paths: string[]) {
  return function Glyph(props: IconProps) {
    return (
      <svg
        viewBox="0 0 24 24"
        width={ICON_SIZE_PX}
        height={ICON_SIZE_PX}
        fill="none"
        stroke="currentColor"
        strokeWidth={ICON_STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        focusable="false"
        {...props}
      >
        {paths.map((d) => (
          <path key={d} d={d} />
        ))}
      </svg>
    );
  };
}

export const Icon = {
  Workflow: glyph(["M6 3v6", "M6 15v6", "M18 9v6", "M6 9a3 3 0 1 0 0 6 3 3 0 1 0 0-6", "M9 12h6", "M18 3a3 3 0 1 0 0 6 3 3 0 1 0 0-6"]),
  Run: glyph(["M6 4l14 8-14 8z"]),
  Approval: glyph(["M4 12l5 5L20 6"]),
  Connection: glyph(["M10 14a4 4 0 0 0 6 0l3-3a4 4 0 0 0-6-6l-1 1", "M14 10a4 4 0 0 0-6 0l-3 3a4 4 0 0 0 6 6l1-1"]),
  Settings: glyph(["M12 9a3 3 0 1 0 0 6 3 3 0 1 0 0-6", "M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M5 19l2-2M17 7l2-2"]),
  Sun: glyph(["M12 8a4 4 0 1 0 0 8 4 4 0 1 0 0-8", "M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5L19 19M5 19l1.5-1.5M17.5 6.5L19 5"]),
  Moon: glyph(["M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"]),
  Monitor: glyph(["M3 5h18v11H3z", "M8 20h8M12 16v4"]),
  Warning: glyph(["M12 3l10 18H2z", "M12 10v4", "M12 17v.5"]),
};

export type IconName = keyof typeof Icon;
