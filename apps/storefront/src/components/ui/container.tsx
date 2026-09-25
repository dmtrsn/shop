import type { ComponentPropsWithoutRef, ElementType, ReactNode } from "react";

import styles from "./ui.module.css";

type ContainerProps<T extends ElementType = "div"> = {
  as?: T;
  children: ReactNode;
  className?: string;
} & Omit<ComponentPropsWithoutRef<T>, "as" | "children" | "className">;

export function Container<T extends ElementType = "div">({
  as,
  children,
  className,
  ...props
}: ContainerProps<T>) {
  const Component = as ?? "div";
  const classes = className ? `${styles.container} ${className}` : styles.container;

  return (
    <Component className={classes} {...props}>
      {children}
    </Component>
  );
}
