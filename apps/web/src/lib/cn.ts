import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

const mergeClassNames = extendTailwindMerge({
  extend: {
    theme: {
      text: [
        "ui-page",
        "ui-section",
        "ui-subsection",
        "ui-body-lg",
        "ui-body",
        "ui-body-sm",
        "ui-caption",
        "ui-label",
      ],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return mergeClassNames(clsx(inputs));
}
