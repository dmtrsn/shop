"use client";

import { useEffect, useState, type AnchorHTMLAttributes } from "react";
import { buildLegacyLoginHref } from "@/lib/return-to";

type Props = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & { href: string };

export function LegacyLoginLink({ href: entry, ...props }: Props) {
  const [href, setHref] = useState(entry);
  function currentHref() {
    const { pathname, search, hash, origin } = window.location;
    return buildLegacyLoginHref(entry, pathname + search + hash, origin);
  }
  // Actual browser URLs already include the compiled basePath. Initial
  // server/client output stays identical; no auth data is needed for this link.
  useEffect(() => { setHref(currentHref()); });
  return <a {...props} href={href} onClick={(event) => {
    // Cover a hash/query change immediately before activation as well.
    event.currentTarget.href = currentHref();
    props.onClick?.(event);
  }} />;
}
