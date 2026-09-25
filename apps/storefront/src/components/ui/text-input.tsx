import { forwardRef, useId, type InputHTMLAttributes, type ReactNode } from "react";

import styles from "./ui.module.css";

export type TextInputProps = InputHTMLAttributes<HTMLInputElement> & {
  hint?: string;
  icon?: ReactNode;
  label: string;
  visuallyHideLabel?: boolean;
};

export const TextInput = forwardRef<HTMLInputElement, TextInputProps>(function TextInput(
  { className, hint, icon, id, label, visuallyHideLabel = false, ...props },
  ref,
) {
  const reactId = useId();
  const generatedId = id ?? `field-${reactId.replaceAll(":", "")}`;
  const hintId = hint ? `${generatedId}-hint` : undefined;
  const inputClasses = [styles.input, icon ? styles.inputWithIcon : "", className]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={styles.field}>
      <label className={visuallyHideLabel ? "visually-hidden" : styles.label} htmlFor={generatedId}>
        {label}
      </label>
      <span className={styles.inputWrap}>
        {icon ? <span className={styles.inputIcon}>{icon}</span> : null}
        <input
          ref={ref}
          aria-describedby={hintId}
          className={inputClasses}
          id={generatedId}
          {...props}
        />
      </span>
      {hint ? (
        <span className={styles.hint} id={hintId}>
          {hint}
        </span>
      ) : null}
    </div>
  );
});
