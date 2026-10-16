import type { ComponentPropsWithRef, ReactNode } from "react";

import styles from "./primitives.module.css";

type EmptyStateProps = {
  title: ReactNode;
  children?: ReactNode;
  icon?: ReactNode;
  actions?: ReactNode;
  level?: 2 | 3;
  className?: string;
  // For focus management on the heading (ref, tabIndex, id).
  titleProps?: Omit<ComponentPropsWithRef<"h2">, "children" | "className">;
};

// Honest empty/unavailable state: what happened and one clear next step.
export function EmptyState({ title, children, icon, actions, level = 2, className, titleProps }: EmptyStateProps) {
  const Heading = level === 3 ? "h3" : "h2";
  return (
    <section className={className ? `${styles.emptyState} ${className}` : styles.emptyState}>
      {icon ? <span aria-hidden="true" className={styles.emptyIcon}>{icon}</span> : null}
      <Heading className={styles.emptyTitle} {...titleProps}>{title}</Heading>
      {children ? <div className={styles.emptyText}>{children}</div> : null}
      {actions ? <div className={styles.emptyActions}>{actions}</div> : null}
    </section>
  );
}
