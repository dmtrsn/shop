"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { mapLegacyHash } from "@/lib/legacy-hash";

export function LegacyHashBridge() {
  const router = useRouter();
  const pathname = usePathname();
  useEffect(() => {
    let lastHash: string | null = null;
    const migrate = () => {
      const hash = window.location.hash;
      if (hash === lastHash) return;
      lastHash = hash;
      const target = mapLegacyHash(hash);
      if (target === null) return;
      if (target === pathname + window.location.search) {
        // Already on the target: router.replace would be a same-page no-op that
        // keeps the stale fragment, so drop only the hash (Next syncs this).
        window.history.replaceState(null, "", window.location.pathname + window.location.search);
      } else {
        // Next adds basePath. replace drops the hash and avoids a Back-button loop.
        router.replace(target);
      }
    };
    migrate();
    window.addEventListener("hashchange", migrate);
    return () => window.removeEventListener("hashchange", migrate);
  }, [router, pathname]);
  return null;
}
