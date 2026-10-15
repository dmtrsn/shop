import Link from "next/link";
import type { ButtonHTMLAttributes, ComponentProps, ReactNode } from "react";

import styles from "./ui.module.css";

export type ButtonVariant = "primary" | "brand" | "secondary" | "quiet" | "tertiary";
export type ButtonSize = "md" | "sm";

const variants: Record<ButtonVariant, string> = {
  primary: styles.buttonPrimary,
  brand: styles.buttonBrand,
  secondary: styles.buttonSecondary,
  quiet: styles.buttonQuiet,
  tertiary: styles.buttonTertiary,
};

// Shared by Button and ButtonLink so a link can look like an action.
export function buttonClassName(variant: ButtonVariant = "primary", size: ButtonSize = "md", className?: string): string {
  return [styles.button, variants[variant], size === "sm" ? styles.buttonSmall : "", className].filter(Boolean).join(" ");
}

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  children: ReactNode;
  variant?: ButtonVariant;
  size?: ButtonSize;
};

export function Button({
  children,
  className,
  type = "button",
  variant = "primary",
  size = "md",
  ...props
}: ButtonProps) {
  return (
    <button className={buttonClassName(variant, size, className)} type={type} {...props}>
      {children}
    </button>
  );
}

export type ButtonLinkProps = ComponentProps<typeof Link> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
};

// Navigation styled as an action. It stays a real link: no button role.
export function ButtonLink({ className, variant = "primary", size = "md", ...props }: ButtonLinkProps) {
  return <Link className={buttonClassName(variant, size, className)} {...props} />;
}
