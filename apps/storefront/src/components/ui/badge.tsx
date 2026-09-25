import type { HTMLAttributes, ReactNode } from "react";

import styles from "./ui.module.css";

type BadgeProps = HTMLAttributes<HTMLSpanElement> & {
  children: ReactNode;
};

export function Badge({ children, className, ...props }: BadgeProps) {
  const classes = className ? `${styles.badge} ${className}` : styles.badge;

  return (
    <span className={classes} {...props}>
      {children}
    </span>
  );
}
