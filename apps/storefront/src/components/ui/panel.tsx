import type { ComponentPropsWithoutRef, ElementType, ReactNode } from "react";

import styles from "./primitives.module.css";

type PanelProps<T extends ElementType = "div"> = {
  as?: T;
  children: ReactNode;
  className?: string;
  tone?: "default" | "subtle" | "inverse";
  padding?: "none" | "md" | "lg";
  // Hover lift for independently actionable cards; never at rest.
  lift?: boolean;
} & Omit<ComponentPropsWithoutRef<T>, "as" | "children" | "className">;

const tones = { default: "", subtle: styles.panelSubtle, inverse: styles.panelInverse };
const paddings = { none: "", md: styles.padMd, lg: styles.padLg };

// Bordered surface with the panel radius and no resting shadow.
export function Panel<T extends ElementType = "div">({
  as, children, className, tone = "default", padding = "md", lift = false, ...props
}: PanelProps<T>) {
  const Component = as ?? "div";
  const classes = [styles.panel, tones[tone], paddings[padding], lift ? styles.lift : "", className].filter(Boolean).join(" ");
  return <Component className={classes} {...props}>{children}</Component>;
}
