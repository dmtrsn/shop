import type { ReactNode } from "react";

import { Button, ButtonLink } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { SearchIcon } from "@/components/ui/icons";
import { Skeleton } from "@/components/ui/skeleton";
import { UnavailableState } from "@/components/ui/unavailable-state";

import styles from "./route-states.module.css";

type RouteLink = { href: string; label: string; prefetch?: false };

// Route error boundary body: a failure, never "empty" or "signed out". Retry
// uses Next's reset; the digest is an opaque reference, never backend detail.
export function RouteError({ title, children, reset, link, digest }: {
  title: string;
  children: ReactNode;
  reset?: () => void;
  link?: RouteLink;
  digest?: string;
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container>
        <UnavailableState
          actions={reset || link ? <>
            {reset ? <Button onClick={reset} variant="secondary">Повторить</Button> : null}
            {link ? <ButtonLink href={link.href} prefetch={link.prefetch} variant="tertiary">{link.label}</ButtonLink> : null}
          </> : undefined}
          level={1}
          role="alert"
          title={title}
        >
          <p>{children}</p>
          {digest ? <p className={styles.reference}>Код обращения: {digest}</p> : null}
        </UnavailableState>
      </Container>
    </main>
  );
}

// Not-found body for the global 404 and route-local missing resources.
export function RouteNotFound({ title, children, links }: { title: string; children: ReactNode; links: RouteLink[] }) {
  return (
    <main className={styles.main} id="main-content">
      <Container>
        <EmptyState
          actions={links.map((link, index) => (
            <ButtonLink href={link.href} key={link.href} prefetch={link.prefetch} variant={index === 0 ? "primary" : "secondary"}>{link.label}</ButtonLink>
          ))}
          icon={<SearchIcon />}
          level={1}
          title={title}
        >
          {children}
        </EmptyState>
      </Container>
    </main>
  );
}

// Decorative rows for directory/detail loading fallbacks; the page owns aria-busy.
export function SkeletonRows({ count = 4 }: { count?: number }) {
  return <div aria-hidden="true" className={styles.rows}>
    {Array.from({ length: count }, (_, index) => <Skeleton className={styles.row} key={index} variant="block" />)}
  </div>;
}
