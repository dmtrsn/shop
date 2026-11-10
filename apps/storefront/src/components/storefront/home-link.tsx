import Link from "next/link";
import type { ComponentProps } from "react";

type Props = Omit<ComponentProps<typeof Link>, "href" | "prefetch">;

// Prefetching the home loading boundary also downloads its high-priority hero.
// Fetch it only when home is actually opened.
export function HomeLink(props: Props) {
  return <Link {...props} href="/" prefetch={false} />;
}
