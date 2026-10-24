"use client";

import { useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import { getStaffSession, logoutStaff, type Staff } from "@/lib/staff-api";

export type CsrfResult = { status: "ready"; csrfToken: string } | { status: "ended" | "changed" | "unavailable" };

type StaffSessionValue = {
  staff: Staff;
  /** The staff CSRF token for unsafe requests, fetched from Go when missing. */
  csrfToken: () => Promise<CsrfResult>;
  /** Fetches a fresh token from Go, for the single retry after a CSRF rejection. */
  refreshCsrfToken: () => Promise<CsrfResult>;
  /** Ends the session; resolves to a message to show, or null after leaving. */
  signOut: () => Promise<string | null>;
  /** The session ended (expired or revoked): drop state and show the sign-in page. */
  sessionEnded: () => void;
};

const StaffSessionContext = createContext<StaffSessionValue | null>(null);

export const signOutMessages = {
  changed: "В этом браузере выполнен вход под другой учётной записью сотрудника. Обновите страницу.",
  unavailable: "Не удалось выйти: сервис недоступен. Повторите попытку.",
} as const;

/**
 * The signed-in staff member of this page. The server gate supplies the
 * identity (never a secret); the CSRF token is fetched from Go and kept only
 * in this component's memory, never in storage, cookies or the URL. A session
 * that Go no longer recognises sends the browser to /login once.
 */
export function StaffSessionProvider({ staff, children }: { staff: Staff; children: ReactNode }) {
  const router = useRouter();
  const [csrf, setCsrf] = useState<string | null>(null);

  const sessionEnded = useCallback(() => {
    setCsrf(null);
    router.replace("/login");
    router.refresh();
  }, [router]);

  const load = useCallback(
    async (signal?: AbortSignal): Promise<CsrfResult> => {
      const session = await getStaffSession(fetch, signal);
      if (signal?.aborted) return { status: "unavailable" };
      if (session.status === "anonymous") {
        sessionEnded();
        return { status: "ended" };
      }
      if (session.status === "unavailable") return { status: "unavailable" };
      if (session.staff.id !== staff.id) {
        // Someone else signed in from another tab: re-render from the server.
        setCsrf(null);
        router.refresh();
        return { status: "changed" };
      }
      setCsrf(session.csrfToken);
      return { status: "ready", csrfToken: session.csrfToken };
    },
    [router, sessionEnded, staff.id],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const csrfToken = useCallback(async (): Promise<CsrfResult> => (csrf ? { status: "ready", csrfToken: csrf } : load()), [csrf, load]);

  const signOut = useCallback(async (): Promise<string | null> => {
    let token = await csrfToken();
    if (token.status === "ended") return null;
    if (token.status !== "ready") return token.status === "changed" ? signOutMessages.changed : signOutMessages.unavailable;
    let result = await logoutStaff(token.csrfToken, staff.id);
    if (result.status === "stale-csrf") {
      // A newer sign-in replaced the cookie; its token is Go's to give.
      setCsrf(null);
      token = await load();
      if (token.status === "ended") return null;
      if (token.status !== "ready") return token.status === "changed" ? signOutMessages.changed : signOutMessages.unavailable;
      result = await logoutStaff(token.csrfToken, staff.id);
    }
    switch (result.status) {
      case "signed-out":
      case "expired":
        sessionEnded();
        return null;
      case "changed":
        return signOutMessages.changed;
      default:
        return signOutMessages.unavailable;
    }
  }, [csrfToken, load, sessionEnded, staff.id]);

  const refreshCsrfToken = useCallback(() => {
    setCsrf(null);
    return load();
  }, [load]);

  const value = useMemo(
    () => ({ staff, csrfToken, refreshCsrfToken, signOut, sessionEnded }),
    [staff, csrfToken, refreshCsrfToken, signOut, sessionEnded],
  );
  return <StaffSessionContext.Provider value={value}>{children}</StaffSessionContext.Provider>;
}

export function useStaffSession(): StaffSessionValue {
  const value = useContext(StaffSessionContext);
  if (!value) throw new Error("useStaffSession needs StaffSessionProvider");
  return value;
}
