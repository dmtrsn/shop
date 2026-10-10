"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ComponentProps } from "react";

import { buildLoginHref } from "@/lib/return-to";

type Props = Omit<ComponentProps<typeof Link>, "href" | "prefetch">;

// Sign-in entry for every signed-out private surface: /login?returnTo=<route>.
// The target is the app path (Link and redirect() add any basePath), plus the
// browser query and fragment once mounted. It needs no auth data or request.
export function LoginLink(props: Props) {
  // Outside the App Router (unit renders) there is no pathname; use the browser's.
  const pathname = usePathname();
  const [href, setHref] = useState(() => buildLoginHref(pathname ?? "/"));
  useEffect(() => {
    const update = () => {
      const { search, hash } = window.location;
      setHref(buildLoginHref((pathname ?? window.location.pathname) + search + hash));
    };
    // Every render may follow a query change; same-value updates are skipped.
    update();
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  });
  return <Link {...props} href={href} prefetch={false} />;
}
