"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { mapLegacyHash } from "@/lib/legacy-hash";

export function LegacyHashBridge() {
  const router = useRouter();
  useEffect(() => {
    let lastHash: string | null = null;
    const migrate = () => {
      const hash = window.location.hash;
      if (hash === lastHash) return;
      lastHash = hash;
      const target = mapLegacyHash(hash);
      // Next adds basePath. replace drops the hash and avoids a Back-button loop.
      if (target !== null) router.replace(target);
    };
    migrate();
    window.addEventListener("hashchange", migrate);
    return () => window.removeEventListener("hashchange", migrate);
  }, [router]);
  return null;
}
