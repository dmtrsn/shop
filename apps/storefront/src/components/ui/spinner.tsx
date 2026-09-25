import type { HTMLAttributes } from "react";

import styles from "./ui.module.css";

type SpinnerProps = HTMLAttributes<HTMLSpanElement> & {
  label?: string;
};

export function Spinner({ className, label = "Загрузка", ...props }: SpinnerProps) {
  const classes = className ? `${styles.spinner} ${className}` : styles.spinner;

  return (
    <span aria-label={label} className={classes} role="status" {...props}>
      <span className="visually-hidden">{label}</span>
    </span>
  );
}
