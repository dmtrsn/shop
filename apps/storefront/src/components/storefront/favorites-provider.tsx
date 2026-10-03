"use client";

import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { Favorites } from "@/lib/api/favorites";
import {
  FavoritesRequestError, getBrowserFavorites, getBrowserSession, setBrowserFavorite,
  type BrowserSession,
} from "@/lib/api/favorites-client";

import styles from "./favorites.module.css";

export type FavoritesRouteSession =
  | { status: "authenticated"; userID: string }
  | { status: "anonymous" | "unavailable" };

type State = {
  status: "idle" | "loading" | "ready" | "anonymous" | "unavailable";
  userID: string | null;
  snapshot: { ownerUserID: string; favorites: Favorites } | null;
  mutationPending: boolean;
  pendingProductID: string | null;
  message: string;
};
const initial: State = {
  status: "idle", userID: null, snapshot: null, mutationPending: false, pendingProductID: null, message: "",
};
type ReadJob = { generation: number; controller: AbortController; sessionPending: boolean; routeUserID?: string; promise: Promise<void> };
type WriteJob = { generation: number; userID: string; productID: string; networkDone: Promise<void> };
type Runtime = {
  state: State;
  session: Extract<BrowserSession, { status: "authenticated" }> | null;
  generation: number;
  read: ReadJob | null;
  write: WriteJob | null;
  alive: boolean;
};

type FavoritesContextValue = State & {
  loginHref: string | null;
  favorites: Favorites | null;
  ensure: () => Promise<void>;
  refresh: () => Promise<void>;
  enterRoute: (session: FavoritesRouteSession) => void;
  toggle: (productID: string) => Promise<void>;
  signInRequired: () => void;
};
const FavoritesContext = createContext<FavoritesContextValue | null>(null);

export function FavoritesProvider({ children, loginHref }: { children: ReactNode; loginHref: string | null }) {
  const [state, setState] = useState<State>(initial);
  const runtime = useRef<Runtime>({ state: initial, session: null, generation: 0, read: null, write: null, alive: true });

  const actions = useMemo(() => {
    const rt = runtime.current;
    function publish(next: State) {
      rt.state = next;
      if (rt.alive) setState(next);
    }
    function message(text: string) { publish({ ...rt.state, message: text }); }
    function current(generation: number, userID?: string) {
      return rt.alive && rt.generation === generation && (!userID || rt.session?.userID === userID);
    }
    // Invalidate the owner and snapshot atomically. Async work must pass current()
    // before it may publish, even if its fetch implementation ignores cancellation.
    function invalidate(status: State["status"], text = rt.state.message) {
      rt.generation++;
      rt.read?.controller.abort();
      rt.read = null;
      rt.session = null;
      publish({ ...initial, status, mutationPending: Boolean(rt.write), message: text });
    }
    function signedOut() { invalidate("anonymous", "Сессия завершена. Войдите снова, чтобы изменить избранное."); }

    async function readFavorites(generation: number, userID: string, signal?: AbortSignal) {
      try {
        const favorites = await getBrowserFavorites({ signal });
        if (!current(generation, userID)) return;
        publish({ ...rt.state, status: "ready", userID, snapshot: { ownerUserID: userID, favorites } });
      } catch (error) {
        if (!current(generation, userID)) return;
        if (error instanceof FavoritesRequestError && error.status === 401) signedOut();
        else invalidate("unavailable", "Не удалось проверить избранное. Повторите загрузку перед следующим действием.");
      }
    }

    function refresh(force = true, routeUserID?: string): Promise<void> {
      // Share the session request. A deliberate refresh can supersede a slow
      // favorites GET, but never starts parallel session/CSRF requests.
      // A newly observed server route identity supersedes an older session
      // check, including CSRF recovery. Effect replay for that route shares it.
      if (rt.read && (!force || rt.read.sessionPending) &&
          (!routeUserID || rt.read.routeUserID === routeUserID || rt.state.userID === routeUserID)) return rt.read.promise;
      invalidate("loading");
      const job: ReadJob = {
        generation: rt.generation, controller: new AbortController(), sessionPending: true, routeUserID, promise: Promise.resolve(),
      };
      rt.read = job;
      job.promise = (async () => {
        const session = await getBrowserSession({ signal: job.controller.signal });
        if (!current(job.generation)) return;
        job.sessionPending = false;
        if (session.status !== "authenticated") {
          invalidate(session.status, session.status === "unavailable"
            ? "Не удалось проверить вход. Попробуйте ещё раз."
            : rt.state.message);
          return;
        }
        rt.session = session;
        publish({ ...rt.state, userID: session.userID });
        // A visibility/session check may run during a write, so an account
        // change can hide private data immediately. For the same user, wait
        // for that network write to settle before reading the resulting list.
        if (rt.write?.userID === session.userID) await rt.write.networkDone;
        if (current(job.generation, session.userID)) {
          await readFavorites(job.generation, session.userID, job.controller.signal);
        }
      })().finally(() => { if (rt.read === job) rt.read = null; });
      return job.promise;
    }

    async function reconcile(generation: number, userID: string) {
      if (!current(generation, userID)) return;
      // Unknown outcomes are never presented as a rollback or a successful
      // local edit. Hide membership until Go answers the read-only check.
      publish({ ...rt.state, status: "loading", snapshot: null });
      await readFavorites(generation, userID);
    }

    async function toggle(productID: string) {
      const session = rt.session;
      const snapshot = rt.state.snapshot;
      if (rt.write || rt.read || rt.state.status !== "ready" || !session || snapshot?.ownerUserID !== session.userID) return;
      const enabled = !snapshot.favorites.items.some((product) => product.id === productID);
      let networkFinished!: () => void;
      const job: WriteJob = {
        generation: rt.generation, userID: session.userID, productID,
        networkDone: new Promise<void>((resolve) => { networkFinished = resolve; }),
      };
      // Synchronous lock: even another click in this same render cannot write.
      rt.write = job;
      publish({ ...rt.state, mutationPending: true, pendingProductID: productID, message: "Сохраняем избранное…" });
      try {
        const favorites = await setBrowserFavorite(productID, enabled, session.csrfToken).finally(networkFinished);
        if (!current(job.generation, job.userID)) return;
        publish({
          ...rt.state, snapshot: { ownerUserID: job.userID, favorites },
          message: enabled ? "Товар добавлен в избранное." : "Товар удалён из избранного.",
        });
      } catch (error) {
        if (!current(job.generation, job.userID)) return;
        if (error instanceof FavoritesRequestError && error.status === 401) {
          signedOut();
        } else if (error instanceof FavoritesRequestError && error.status === 403 && error.code === "csrf_failed") {
          message("Действие не выполнено. Проверяем вход; после проверки повторите действие.");
          // Only reads follow a rejected write. A changed user receives a new
          // owned snapshot, never the previous user's intent or response.
          await refresh();
        } else if (error instanceof FavoritesRequestError && error.status === 404) {
          message("Товар больше недоступен. Обновляем избранное.");
          await reconcile(job.generation, job.userID);
        } else if (error instanceof FavoritesRequestError && error.status && error.status >= 400 && error.status < 500) {
          message("Действие не выполнено. Попробуйте ещё раз.");
        } else {
          message("Не удалось подтвердить изменение. Проверяем сохранённое избранное.");
          await reconcile(job.generation, job.userID);
        }
      } finally {
        if (rt.write === job) {
          rt.write = null;
          publish({ ...rt.state, mutationPending: false, pendingProductID: null,
            message: rt.state.message === "Сохраняем избранное…" ? "" : rt.state.message });
        }
      }
    }

    return {
      ensure: () => rt.state.status === "idle" ? refresh(false) : rt.read?.promise ?? Promise.resolve(),
      refresh: () => refresh(),
      enterRoute: (session: FavoritesRouteSession) => {
        if (session.status === "authenticated") void refresh(true, session.userID);
        else invalidate(session.status, "");
      },
      toggle,
      signInRequired: () => message("Чтобы сохранять избранное, войдите в свой аккаунт."),
    };
  }, []);

  useEffect(() => {
    const rt = runtime.current;
    rt.alive = true;
    let wasHidden = document.visibilityState === "hidden";
    let lastRevalidation = 0;
    function revalidate() {
      const now = Date.now();
      if (rt.state.status !== "idle" && now - lastRevalidation >= 1_000) {
        lastRevalidation = now;
        void actions.refresh();
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
      rt.alive = false;
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pageshow", restored);
      // React's development effect replay reattaches immediately. Keep that
      // initialization shared, but dispose real unmounts and ignore responses.
      queueMicrotask(() => {
        if (!rt.alive) { rt.generation++; rt.read?.controller.abort(); rt.session = null; }
      });
    };
  }, [actions]);

  const favorites = state.status === "ready" && state.userID && state.snapshot?.ownerUserID === state.userID
    ? state.snapshot.favorites : null;
  return (
    <FavoritesContext.Provider value={{ ...state, ...actions, favorites, loginHref }}>
      {children}
      <div aria-atomic="true" aria-live="polite" className={styles.feedback} role="status">
        {state.message ? <span>{state.message}</span> : null}
      </div>
    </FavoritesContext.Provider>
  );
}

export function useFavorites() {
  const value = useContext(FavoritesContext);
  if (!value) throw new Error("Favorite controls require FavoritesProvider");
  return value;
}
