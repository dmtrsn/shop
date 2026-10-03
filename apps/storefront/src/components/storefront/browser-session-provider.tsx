"use client";

import { createContext, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";

import { createBrowserSession, type BrowserSessionStore } from "@/lib/api/browser-session";

const BrowserSessionContext = createContext<BrowserSessionStore | null>(null);

// Initializes nothing by itself: header links and other passive UI never issue
// session requests. Private controls call ensure() lazily.
export function BrowserSessionProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createBrowserSession);
  const alive = useRef(false);

  useEffect(() => {
    alive.current = true;
    let wasHidden = document.visibilityState === "hidden";
    let lastRevalidation = 0;
    // One bounded identity check when a used session returns from a hidden tab
    // or the back/forward cache. Features reread only after it confirms identity.
    function revalidate() {
      const now = Date.now();
      if (store.get().status !== "idle" && now - lastRevalidation >= 1_000) {
        lastRevalidation = now;
        void store.refresh();
      }
    }
    function visibility() {
      if (document.visibilityState === "hidden") wasHidden = true;
      else if (wasHidden) { wasHidden = false; revalidate(); }
    }
    function restored(event: PageTransitionEvent) { if (event.persisted) revalidate(); }
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pageshow", restored);
    return () => {
      alive.current = false;
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pageshow", restored);
      // React's development effect replay reattaches immediately. Keep that
      // initialization shared, but dispose real unmounts.
      queueMicrotask(() => { if (!alive.current) store.dispose(); });
    };
  }, [store]);

  return <BrowserSessionContext.Provider value={store}>{children}</BrowserSessionContext.Provider>;
}

export function useBrowserSessionStore() {
  const store = useContext(BrowserSessionContext);
  if (!store) throw new Error("Private controls require BrowserSessionProvider");
  return store;
}

export function useBrowserSession() {
  const store = useBrowserSessionStore();
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}
