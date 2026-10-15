import styles from "./primitives.module.css";

type LogoMarkProps = {
  className?: string;
  // Footer and other dark bands use the inverted mark.
  tone?: "default" | "inverse";
  size?: number;
  wordmark?: boolean;
  // Accessible name when the mark stands alone; omit when the context names it.
  label?: string;
};

// The original "слой" mark: a rounded dark square with a lime downward triangle.
export function LogoMark({ className, tone = "default", size = 39, wordmark = false, label }: LogoMarkProps) {
  const classes = [styles.logo, tone === "inverse" ? styles.logoInverse : "", className].filter(Boolean).join(" ");
  const named = label && !wordmark ? { role: "img", "aria-label": label } : {};
  return (
    <span className={classes} {...named}>
      <svg aria-hidden="true" focusable="false" height={size} viewBox="0 0 42 42" width={size}>
        <rect className={styles.logoSquare} height="42" rx="13" width="42" />
        <path className={styles.logoTriangle} d="M10 14.5h22L21 29z" />
      </svg>
      {wordmark ? <span className={styles.wordmark}>слой</span> : null}
    </span>
  );
}
