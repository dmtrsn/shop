import type { ComponentPropsWithRef, ReactNode } from "react";

import styles from "./primitives.module.css";

type PageHeaderProps = {
  title: ReactNode;
  eyebrow?: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  className?: string;
  // For focus management on the page's single h1 (ref, tabIndex, id).
  titleProps?: Omit<ComponentPropsWithRef<"h1">, "children" | "className">;
};

// The page's only h1, with optional eyebrow, subtitle and actions.
export function PageHeader({ title, eyebrow, subtitle, actions, className, titleProps }: PageHeaderProps) {
  return (
    <header className={className ? `${styles.pageHeader} ${className}` : styles.pageHeader}>
      <div>
        {eyebrow ? <p className={styles.eyebrow}>{eyebrow}</p> : null}
        <h1 className={styles.pageTitle} {...titleProps}>{title}</h1>
        {subtitle ? <p className={styles.pageSubtitle}>{subtitle}</p> : null}
      </div>
      {actions ? <div className={styles.headerActions}>{actions}</div> : null}
    </header>
  );
}
