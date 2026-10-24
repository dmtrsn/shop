"use client";

import { useCallback } from "react";

import type { ApiFailure, ApiResult, MutationAuth } from "@/lib/admin-api";
import { useStaffSession, type CsrfResult } from "./staff-session";

function csrfFailure(token: CsrfResult): ApiFailure | null {
  switch (token.status) {
    case "ready":
      return null;
    case "ended":
      return { kind: "unauthorized" };
    case "changed":
      return { kind: "staff_changed" };
    default:
      return { kind: "unavailable" };
  }
}

/**
 * The staff session policy for data requests, in one place: an expired
 * session goes to sign-in (once, through the session provider); unsafe
 * requests carry the in-memory CSRF token and the expected staff ID, and a
 * CSRF rejection, which Go returns before any change, is retried once with a
 * fresh token. Nothing else is retried.
 */
export function useStaffApi() {
  const { staff, csrfToken, refreshCsrfToken, sessionEnded } = useStaffSession();

  const read = useCallback(
    async <T,>(request: () => Promise<ApiResult<T>>): Promise<ApiResult<T>> => {
      const result = await request();
      if (!result.ok && result.error.kind === "unauthorized") sessionEnded();
      return result;
    },
    [sessionEnded],
  );

  const mutate = useCallback(
    async <T,>(request: (auth: MutationAuth) => Promise<ApiResult<T>>): Promise<ApiResult<T>> => {
      let token = await csrfToken();
      let failure = csrfFailure(token);
      if (failure || token.status !== "ready") return { ok: false, error: failure ?? { kind: "unavailable" } };
      let result = await request({ csrfToken: token.csrfToken, staffId: staff.id });
      if (!result.ok && result.error.kind === "csrf") {
        token = await refreshCsrfToken();
        failure = csrfFailure(token);
        if (failure || token.status !== "ready") return { ok: false, error: failure ?? { kind: "unavailable" } };
        result = await request({ csrfToken: token.csrfToken, staffId: staff.id });
      }
      if (!result.ok && result.error.kind === "unauthorized") sessionEnded();
      return result;
    },
    [csrfToken, refreshCsrfToken, sessionEnded, staff.id],
  );

  return { read, mutate };
}
