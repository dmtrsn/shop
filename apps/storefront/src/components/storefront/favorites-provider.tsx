"use client";

import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Favorites } from "@/lib/api/favorites";
import { FavoritesRequestError, getBrowserFavorites, setBrowserFavorite } from "@/lib/api/favorites-client";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import styles from "./favorites.module.css";

export type FavoritesRouteSession = RouteSession;

type State = {
  status: "idle" | "loading" | "ready" | "anonymous" | "unavailable";
  userID: string | null;
  snapshot: { ownerUserID: string; generation: number; favorites: Favorites } | null;
  mutationPending: boolean;
  pendingProductID: string | null;
  message: string;
};
const initial: State = {
  status: "idle", userID: null, snapshot: null, mutationPending: false, pendingProductID: null, message: "",
};
type WriteJob = { userID: string; networkDone: Promise<void> };
type Runtime = {
  state: State;
  initialized: boolean;
  alive: boolean;
  generation: number;
  seenSession: string;
  read: AbortController | null;
  reading: Promise<void>;
  write: WriteJob | null;
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

const saving = "Сохраняем избранное…";

export function FavoritesProvider({ children, loginHref }: { children: ReactNode; loginHref: string | null }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const [state, setState] = useState<State>(initial);
  const runtime = useRef<Runtime>({
    state: initial, initialized: false, alive: true, generation: 0, seenSession: "",
    read: null, reading: Promise.resolve(), write: null,
  });

  const actions = useMemo(() => {
    const rt = runtime.current;
    function publish(next: State) {
      rt.state = next;
      if (rt.alive) setState(next);
    }
    function message(text: string) { publish({ ...rt.state, message: text }); }
    // A response may publish only for the feature generation, shared session
    // generation and user that issued it, even if its fetch ignored abort.
    function current(generation: number, sessionGeneration: number, userID: string) {
      const shared = store.get();
      return rt.alive && rt.generation === generation && shared.generation === sessionGeneration &&
        shared.status === "authenticated" && shared.userID === userID;
    }
    function signedOut() {
      message("Сессия завершена. Войдите снова, чтобы изменить избранное.");
      store.invalidate("anonymous");
    }
    async function ownerMismatch(sessionGeneration: number) {
      message("Не удалось подтвердить владельца избранного. Проверяем вход.");
      await store.recoverOwner(sessionGeneration);
    }

    async function read(userID: string, sessionGeneration: number) {
      rt.read?.abort();
      const controller = new AbortController();
      rt.read = controller;
      const generation = ++rt.generation;
      // Unknown outcomes are never presented as a rollback or a successful
      // local edit. Hide membership until Go answers the read-only check.
      publish({ ...rt.state, status: "loading", userID, snapshot: null });
      // For the same user, wait for an in-flight write to settle first.
      if (rt.write?.userID === userID) await rt.write.networkDone;
      if (!current(generation, sessionGeneration, userID)) return;
      try {
        const favorites = await getBrowserFavorites({ expectedUserID: userID, signal: controller.signal });
        if (!current(generation, sessionGeneration, userID)) return;
        publish({
          ...rt.state, status: "ready", userID,
          snapshot: { ownerUserID: userID, generation: sessionGeneration, favorites },
        });
      } catch (error) {
        if (!current(generation, sessionGeneration, userID)) return;
        if (error instanceof ResponseOwnerError) await ownerMismatch(sessionGeneration);
        else if (error instanceof FavoritesRequestError && error.status === 401) signedOut();
        else {
          publish({
            ...rt.state, status: "unavailable", userID: null, snapshot: null,
            message: "Не удалось проверить избранное. Повторите загрузку перед следующим действием.",
          });
        }
      } finally {
        if (rt.read === controller) rt.read = null;
      }
    }

    // Follows the shared session. Any identity transition hides the previous
    // snapshot synchronously; an authenticated identity is read once.
    function sync() {
      if (!rt.initialized) return;
      const shared = store.get();
      const key = `${shared.generation}:${shared.status}`;
      if (rt.seenSession === key) return;
      rt.seenSession = key;
      rt.generation++;
      rt.read?.abort();
      rt.read = null;
      const text = shared.status === "unavailable" && rt.state.status !== "unavailable"
        ? "Не удалось проверить вход. Попробуйте ещё раз."
        : rt.state.message;
      publish({
        ...initial, status: shared.status === "authenticated" ? "loading" : shared.status,
        mutationPending: Boolean(rt.write), message: text,
      });
      if (shared.status === "authenticated") rt.reading = read(shared.userID, shared.generation);
    }

    async function initialize(request: Promise<void>) {
      rt.initialized = true;
      sync();
      await request;
      sync();
      await rt.reading;
    }

    async function toggle(productID: string) {
      const shared = store.get();
      const snapshot = rt.state.snapshot;
      if (rt.write || rt.read || rt.state.status !== "ready" || shared.status !== "authenticated" ||
          snapshot?.ownerUserID !== shared.userID || snapshot.generation !== shared.generation) return;
      const generation = rt.generation;
      const enabled = !snapshot.favorites.items.some((product) => product.id === productID);
      let networkFinished!: () => void;
      const job: WriteJob = {
        userID: shared.userID, networkDone: new Promise<void>((resolve) => { networkFinished = resolve; }),
      };
      // Synchronous lock: even another click in this same render cannot write.
      rt.write = job;
      publish({ ...rt.state, mutationPending: true, pendingProductID: productID, message: saving });
      try {
        const favorites = await setBrowserFavorite(productID, enabled, shared.csrfToken, { expectedUserID: shared.userID })
          .finally(networkFinished);
        if (!current(generation, shared.generation, shared.userID)) return;
        publish({
          ...rt.state, snapshot: { ownerUserID: shared.userID, generation: shared.generation, favorites },
          message: enabled ? "Товар добавлен в избранное." : "Товар удалён из избранного.",
        });
      } catch (error) {
        if (!current(generation, shared.generation, shared.userID)) return;
        if (error instanceof ResponseOwnerError) {
          await ownerMismatch(shared.generation);
        } else if (error instanceof FavoritesRequestError && error.status === 401) {
          signedOut();
        } else if (error instanceof FavoritesRequestError && error.status === 403 && error.code === "csrf_failed") {
          message("Действие не выполнено. Проверяем вход; после проверки повторите действие.");
          // Only reads follow a rejected write. A changed user receives a new
          // owned snapshot, never the previous user's intent or response.
          await initialize(store.refresh());
        } else if (error instanceof FavoritesRequestError && error.status === 404) {
          message("Товар больше недоступен. Обновляем избранное.");
          await read(shared.userID, shared.generation);
        } else if (error instanceof FavoritesRequestError && error.status && error.status >= 400 && error.status < 500) {
          message("Действие не выполнено. Попробуйте ещё раз.");
        } else {
          message("Не удалось подтвердить изменение. Проверяем сохранённое избранное.");
          await read(shared.userID, shared.generation);
        }
      } finally {
        if (rt.write === job) {
          rt.write = null;
          publish({
            ...rt.state, mutationPending: false, pendingProductID: null,
            message: rt.state.message === saving ? "" : rt.state.message,
          });
        }
      }
    }

    return {
      sync,
      ensure: () => initialize(store.ensure()),
      refresh: () => initialize(store.refresh()),
      enterRoute: (route: FavoritesRouteSession) => {
        void initialize(store.enterRoute(route));
        // The route itself presents anonymous/unavailable; no extra toast.
        if (route.status !== "authenticated") message("");
      },
      toggle,
      signInRequired: () => message("Чтобы сохранять избранное, войдите в свой аккаунт."),
    };
  }, [store]);

  useEffect(() => {
    const rt = runtime.current;
    rt.alive = true;
    const unsubscribe = store.subscribe(actions.sync);
    actions.sync();
    return () => {
      unsubscribe();
      rt.alive = false;
      // React's development effect replay reattaches immediately. Keep that
      // initialization shared, but dispose real unmounts and ignore responses.
      queueMicrotask(() => {
        if (!rt.alive) { rt.generation++; rt.read?.abort(); }
      });
    };
  }, [actions, store]);

  // Render-time ownership: a snapshot is visible only for the exact shared
  // session user and generation that read it.
  const owned = session.status === "authenticated" && state.snapshot?.ownerUserID === session.userID &&
    state.snapshot.generation === session.generation;
  const favorites = state.status === "ready" && owned ? state.snapshot!.favorites : null;
  return (
    <FavoritesContext.Provider value={{ ...state, ...actions, favorites, loginHref }}>
      {children}
      <div aria-atomic="true" aria-label="Уведомления избранного" aria-live="polite" className={styles.feedback} role="status">
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
