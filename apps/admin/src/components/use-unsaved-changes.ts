"use client";

import { useEffect } from "react";

export const unsavedChangesMessage = "Есть несохранённые изменения. Уйти со страницы и потерять их?";

/**
 * While dirty: the browser asks before unload or reload, and a click on any
 * same-origin link in the page asks before the in-app navigation starts. The
 * capture listener runs before Next's link handler, so a refusal stops it.
 * Back/forward buttons are not intercepted (the App Router offers no hook).
 */
export function useUnsavedChanges(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    const click = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const link = (event.target as Element | null)?.closest?.("a[href]");
      if (!(link instanceof HTMLAnchorElement) || link.target === "_blank" || link.hasAttribute("download")) return;
      if (new URL(link.href, window.location.href).origin !== window.location.origin) return;
      if (!window.confirm(unsavedChangesMessage)) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    document.addEventListener("click", click, true);
    return () => {
      window.removeEventListener("beforeunload", beforeUnload);
      document.removeEventListener("click", click, true);
    };
  }, [dirty]);
}
