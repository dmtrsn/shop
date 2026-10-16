import type { ComponentPropsWithoutRef, ReactNode } from "react";

import { InfoIcon } from "./icons";
import styles from "./primitives.module.css";

type UnavailableStateProps = Omit<ComponentPropsWithoutRef<"section">, "title"> & {
  title: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  // 1 when the state replaces the whole page (route error boundaries).
  level?: 1 | 2;
};

// A failure to load or verify, never an empty result: a solid warning panel,
// visually distinct from the dashed EmptyState. Children carry text and retry.
export function UnavailableState({ title, children, actions, level = 2, className, ...props }: UnavailableStateProps) {
  const Heading = level === 1 ? "h1" : "h2";
  return (
    <section className={className ? `${styles.unavailableState} ${className}` : styles.unavailableState} {...props}>
      <InfoIcon aria-hidden="true" className={styles.unavailableIcon} />
      <div className={styles.unavailableBody}>
        <Heading className={level === 1 ? styles.unavailablePageTitle : styles.unavailableTitle}>{title}</Heading>
        {children}
        {actions ? <div className={styles.unavailableActions}>{actions}</div> : null}
      </div>
    </section>
  );
}
