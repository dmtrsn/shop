import Link from "next/link";
import type { ReactNode } from "react";

import styles from "./primitives.module.css";

type SectionHeaderProps = {
  title: ReactNode;
  kicker?: ReactNode;
  level?: 2 | 3;
  id?: string;
  link?: { href: string; label: ReactNode };
  actions?: ReactNode;
  className?: string;
};

// Section heading with an optional green kicker and a trailing "see all" link.
export function SectionHeader({ title, kicker, level = 2, id, link, actions, className }: SectionHeaderProps) {
  const Heading = level === 3 ? "h3" : "h2";
  return (
    <div className={className ? `${styles.sectionHeader} ${className}` : styles.sectionHeader}>
      <div>
        {kicker ? <p className={styles.kicker}>{kicker}</p> : null}
        <Heading className={styles.sectionTitle} id={id}>{title}</Heading>
      </div>
      {link ? (
        <Link className={styles.sectionLink} href={link.href}>
          {link.label} <span aria-hidden="true">→</span>
        </Link>
      ) : null}
      {actions}
    </div>
  );
}
