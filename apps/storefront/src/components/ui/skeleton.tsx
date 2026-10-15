import styles from "./primitives.module.css";

const variants = {
  text: styles.skeletonText,
  title: styles.skeletonTitle,
  block: styles.skeletonBlock,
  media: styles.skeletonMedia,
};

// Decorative placeholder. The surrounding region owns aria-busy/status text.
// Sizes come from variants or a module class: the CSP blocks inline styles.
export function Skeleton({ variant = "text", className }: { variant?: keyof typeof variants; className?: string }) {
  return <span aria-hidden="true" className={[styles.skeleton, variants[variant], className].filter(Boolean).join(" ")} />;
}
