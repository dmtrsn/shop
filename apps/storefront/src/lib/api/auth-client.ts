import { browserNoContent, type PrivateRequestOptions } from "./browser-request";

// One destructive attempt, never retried by the transport.
export function logoutBrowserSession(options: PrivateRequestOptions & { csrfToken: string }): Promise<void> {
  return browserNoContent("/api/auth/session", { ...options, method: "DELETE" });
}
