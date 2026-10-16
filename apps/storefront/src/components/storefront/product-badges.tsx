import styles from "./product-badges.module.css";

// Backend badges only, shown verbatim. Tone follows the text: "Новинка" is lime,
// a reduced-price badge ("-10%", "Скидка …") is the sale tone, anything else neutral.
export function badgeTone(badge: string): string {
  if (/^новинк/i.test(badge)) return `${styles.badge} ${styles.badgeNew}`;
  if (/^[-−]\s?\d+\s?%$|скидк/i.test(badge)) return `${styles.badge} ${styles.badgeSale}`;
  return styles.badge;
}

export function ProductBadges({ badges, className, limit = 2 }: { badges: string[]; className?: string; limit?: number }) {
  if (!badges.length) return null;
  return (
    <span className={className}>
      {badges.slice(0, limit).map((badge) => (
        <span className={badgeTone(badge)} key={badge}>{badge}</span>
      ))}
    </span>
  );
}
