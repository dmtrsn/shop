import { browserRequest, type BrowserRequestOptions } from "./browser-request";
import { isRecord } from "./json";

export type AccountSummary = { name: string; email: string; phone?: string };

export type BrowserSession =
  | { status: "authenticated"; userID: string; csrfToken: string; account: AccountSummary }
  | { status: "anonymous" | "unavailable" };

export type SessionState = (BrowserSession | { status: "idle" | "loading" }) & { generation: number };

// Sanitized server route gate. It never contains CSRF, cookies or tokens.
export type RouteSession = { status: "authenticated"; userID: string } | { status: "anonymous" | "unavailable" };

export function parseBrowserSession(body: unknown): BrowserSession {
  if (!isRecord(body) || typeof body.authenticated !== "boolean") return { status: "unavailable" };
  if (!body.authenticated) return { status: "anonymous" };
  if (!isRecord(body.user) || typeof body.user.id !== "string" || !body.user.id ||
      typeof body.user.name !== "string" || typeof body.user.email !== "string" ||
      (body.user.phone !== undefined && typeof body.user.phone !== "string") ||
      typeof body.csrfToken !== "string" || !body.csrfToken.trim()) return { status: "unavailable" };
  return {
    status: "authenticated", userID: body.user.id, csrfToken: body.csrfToken,
    account: {
      name: body.user.name, email: body.user.email,
      ...(typeof body.user.phone === "string" && body.user.phone.trim() ? { phone: body.user.phone } : {}),
    },
  };
}

export async function getBrowserSession(options: BrowserRequestOptions = {}): Promise<BrowserSession> {
  try { return parseBrowserSession(await browserRequest("/api/auth/session", options)); }
  catch { return { status: "unavailable" }; }
}

type SessionJob = { controller: AbortController; promise: Promise<void>; routeUserID?: string };

// Shared identity for private browser features. It owns status, user ID, the
// allowlisted account summary, in-memory CSRF and generation atomically; features keep their own reads,
// snapshots, writes and messages. One instance per provider, never a module
// singleton, so server rendering cannot share it between requests.
export function createBrowserSession() {
  let state: SessionState = { status: "idle", generation: 0 };
  let job: SessionJob | null = null;
  // Generation produced by the latest owner recovery. A second owner failure in
  // that generation settles to unavailable instead of refreshing again.
  let recoveryGeneration = -1;
  const listeners = new Set<() => void>();

  function publish(next: SessionState) {
    state = next;
    listeners.forEach((listener) => listener());
  }

  // Every identity transition gets a new generation, so responses started
  // under the previous one fail their ownership check even if abort is ignored.
  function invalidate(status: "anonymous" | "unavailable" | "loading") {
    job?.controller.abort();
    job = null;
    publish({ status, generation: state.generation + 1 });
  }

  function refresh(routeUserID?: string): Promise<void> {
    // One session request at a time. A newly observed server route identity
    // supersedes an older check; repeated calls for it share the same request.
    if (job && (!routeUserID || job.routeUserID === routeUserID)) return job.promise;
    invalidate("loading");
    const generation = state.generation;
    const next: SessionJob = { controller: new AbortController(), promise: Promise.resolve(), routeUserID };
    job = next;
    next.promise = (async () => {
      const session = await getBrowserSession({ signal: next.controller.signal });
      if (state.generation === generation) publish({ ...session, generation });
    })().finally(() => { if (job === next) job = null; });
    return next.promise;
  }

  return {
    get: () => state,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    ensure: () => state.status === "idle" ? refresh() : job?.promise ?? Promise.resolve(),
    refresh: () => refresh(),
    invalidate,
    enterRoute: (route: RouteSession) => {
      if (route.status === "authenticated") return refresh(route.userID);
      invalidate(route.status);
      return Promise.resolve();
    },
    // A current-generation private response was missing or bound to another
    // owner. Obsolete generations are ignored; recovery is one refresh.
    recoverOwner: (generation: number) => {
      if (state.generation !== generation) return Promise.resolve();
      if (generation === recoveryGeneration) {
        invalidate("unavailable");
        return Promise.resolve();
      }
      const promise = refresh();
      recoveryGeneration = state.generation;
      return promise;
    },
    dispose: () => {
      job?.controller.abort();
      job = null;
      state = { status: "idle", generation: state.generation + 1 };
    },
  };
}

export type BrowserSessionStore = ReturnType<typeof createBrowserSession>;
