import type { ButtonHTMLAttributes, ReactNode } from "react";

import styles from "./ui.module.css";

type ButtonVariant = "primary" | "secondary" | "tertiary";

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  children: ReactNode;
  variant?: ButtonVariant;
};

const variants: Record<ButtonVariant, string> = {
  primary: styles.buttonPrimary,
  secondary: styles.buttonSecondary,
  tertiary: styles.buttonTertiary,
};

export function Button({
  children,
  className,
  type = "button",
  variant = "primary",
  ...props
}: ButtonProps) {
  const classes = [styles.button, variants[variant], className].filter(Boolean).join(" ");

  return (
    <button className={classes} type={type} {...props}>
      {children}
    </button>
  );
}
