import type { ButtonHTMLAttributes, ReactNode } from "react";

import styles from "./ui.module.css";

type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "aria-label"> & {
  "aria-label": string;
  children: ReactNode;
};

export function IconButton({ children, className, type = "button", ...props }: IconButtonProps) {
  const classes = className ? `${styles.iconButton} ${className}` : styles.iconButton;

  return (
    <button className={classes} type={type} {...props}>
      {children}
    </button>
  );
}
