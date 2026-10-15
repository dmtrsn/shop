"use client";

import type { ReactNode } from "react";

// Horizontal scroll list. Browsers do not scroll a partly visible focused item
// into view, so keyboard focus would sit half outside the rail; this brings the
// focused item fully into view (instantly, so no motion preference applies).
export function ScrollRail({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <ul
      className={className}
      onFocus={(event) => {
        if (event.target instanceof HTMLElement) {
          event.target.scrollIntoView?.({ block: "nearest", inline: "nearest" });
        }
      }}
    >
      {children}
    </ul>
  );
}
