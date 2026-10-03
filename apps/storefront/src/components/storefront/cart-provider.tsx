"use client";

import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Cart } from "@/lib/api/cart";
import { getBrowserCart, mutateBrowserCart, type CartCommand } from "@/lib/api/cart-client";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import styles from "./cart.module.css";

export type CartRouteSession = RouteSession;

// Errors are keyed by canonical product ID, or by this key for bulk selection.
export const selectionKey = "selection";

type State = {
  status: "idle" | "loading" | "ready" | "anonymous" | "unavailable";
  userID: string | null;
  snapshot: { ownerUserID: string; generation: number; cart: Cart } | null;
  mutationPending: boolean;
  pendingProductID: string | null;
  // The visible snapshot predates an uncertain write and is being checked.
  reconciling: boolean;
  errors: Record<string, string>;
  message: string;
};
const initial: State = {
  status: "idle", userID: null, snapshot: null, mutationPending: false, pendingProductID: null,
  reconciling: false, errors: {}, message: "",
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

type CartContextValue = State & {
  loginHref: string | null;
  cart: Cart | null;
  ensure: () => Promise<void>;
  refresh: () => Promise<void>;
  enterRoute: (session: CartRouteSession) => void;
  mutate: (command: CartCommand) => Promise<boolean>;
  selectAll: (selected: boolean) => Promise<boolean>;
  signInRequired: () => void;
};
const CartContext = createContext<CartContextValue | null>(null);

const saving = "Сохраняем корзину…";

function successMessage(command: CartCommand): string {
  switch (command.kind) {
    case "add": return "Товар добавлен в корзину.";
    case "quantity": return "Количество обновлено.";
    case "remove": return "Товар удалён из корзины.";
    case "selection": return command.selected ? "Выбрана вся корзина." : "Выбор снят со всей корзины.";
    // The checkbox itself conveys a single confirmed selection.
    case "selected": return "";
  }
}

function errorKey(command: CartCommand): string {
  return command.kind === "selection" ? selectionKey : command.productID;
}

export function CartProvider({ children, loginHref }: { children: ReactNode; loginHref: string | null }) {
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
      message("Сессия завершена. Войдите снова, чтобы пользоваться корзиной.");
      store.invalidate("anonymous");
    }
    async function ownerMismatch(sessionGeneration: number) {
      message("Не удалось подтвердить владельца корзины. Проверяем вход.");
      await store.recoverOwner(sessionGeneration);
    }

    // Read-only GET. With preserve, the last confirmed snapshot stays visible
    // (marked as being checked) so drafts and focus survive; writes stay blocked
    // because rt.read is set. Without it, the cart is hidden until Go answers.
    async function read(userID: string, sessionGeneration: number, preserve = false): Promise<boolean> {
      rt.read?.abort();
      const controller = new AbortController();
      rt.read = controller;
      const generation = ++rt.generation;
      publish(preserve && rt.state.snapshot
        ? { ...rt.state, reconciling: true }
        : { ...rt.state, status: "loading", userID, snapshot: null, reconciling: false });
      // For the same user, wait for an in-flight write to settle first.
      if (rt.write?.userID === userID) await rt.write.networkDone;
      if (!current(generation, sessionGeneration, userID)) return false;
      try {
        const cart = await getBrowserCart({ expectedUserID: userID, signal: controller.signal });
        if (!current(generation, sessionGeneration, userID)) return false;
        publish({
          ...rt.state, status: "ready", userID, reconciling: false,
          snapshot: { ownerUserID: userID, generation: sessionGeneration, cart },
        });
        return true;
      } catch (error) {
        if (!current(generation, sessionGeneration, userID)) return false;
        if (error instanceof ResponseOwnerError) await ownerMismatch(sessionGeneration);
        else if (error instanceof BrowserRequestError && error.status === 401) signedOut();
        else {
          // Writes stay blocked until a deliberate retry succeeds.
          publish({
            ...rt.state, status: "unavailable", userID: null, snapshot: null, reconciling: false,
            message: "Не удалось загрузить корзину. Повторите загрузку перед следующим действием.",
          });
        }
        return false;
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
      publish({
        ...initial, status: shared.status === "authenticated" ? "loading" : shared.status,
        mutationPending: Boolean(rt.write), message: rt.state.message,
      });
      if (shared.status === "authenticated") rt.reading = read(shared.userID, shared.generation).then(() => {});
    }

    async function initialize(request: Promise<void>) {
      rt.initialized = true;
      sync();
      await request;
      sync();
      await rt.reading;
    }

    async function mutate(command: CartCommand): Promise<boolean> {
      const shared = store.get();
      const snapshot = rt.state.snapshot;
      // One global synchronous write lock: every response is a complete Cart
      // without a revision, so no two writes may overlap, even for different
      // products. Locked attempts are dropped, never queued or replayed.
      if (rt.write || rt.read || rt.state.status !== "ready" || shared.status !== "authenticated" ||
          snapshot?.ownerUserID !== shared.userID || snapshot.generation !== shared.generation) return false;
      const lines = snapshot.cart.items;
      const has = (productID: string) => lines.some((line) => line.product.id === productID);
      if (command.kind === "selection") {
        if (!command.productIDs.length || !command.productIDs.every(has)) return false;
      } else if (command.kind === "add" ? has(command.productID) : !has(command.productID)) {
        return false;
      }
      const key = errorKey(command);
      const generation = rt.generation;
      let networkFinished!: () => void;
      const job: WriteJob = {
        userID: shared.userID, networkDone: new Promise<void>((resolve) => { networkFinished = resolve; }),
      };
      rt.write = job;
      const errors = { ...rt.state.errors };
      delete errors[key];
      publish({ ...rt.state, mutationPending: true, pendingProductID: key, errors, message: saving });
      try {
        const cart = await mutateBrowserCart(command, shared.csrfToken, { expectedUserID: shared.userID })
          .finally(networkFinished);
        if (!current(generation, shared.generation, shared.userID)) return false;
        publish({
          ...rt.state, snapshot: { ownerUserID: shared.userID, generation: shared.generation, cart },
          message: successMessage(command),
        });
        return true;
      } catch (error) {
        if (!current(generation, shared.generation, shared.userID)) return false;
        const status = error instanceof BrowserRequestError ? error.status : undefined;
        const detail = error instanceof BrowserRequestError ? error.detail : undefined;
        // Line errors are announced by the control's own polite region; only
        // errors whose control may disappear also use the shared toast.
        const lineError = (text: string, announce = false) => publish({
          ...rt.state, errors: { ...rt.state.errors, [key]: text }, message: announce ? text : rt.state.message,
        });
        if (error instanceof ResponseOwnerError) {
          await ownerMismatch(shared.generation);
        } else if (status === 401) {
          signedOut();
        } else if (status === 403 && error instanceof BrowserRequestError && error.code === "csrf_failed") {
          message("Действие не выполнено. Проверяем вход; после проверки повторите действие.");
          // Only reads follow a rejected write. A changed user receives a new
          // owned snapshot, never the previous user's intent or response.
          await initialize(store.refresh());
        } else if (status === 404) {
          lineError("Товар больше недоступен или уже удалён из корзины. Обновляем корзину.", true);
          await read(shared.userID, shared.generation, true);
        } else if (status === 409) {
          // Keep the attempted quantity; never clamp it silently.
          lineError(detail || "Доступный остаток изменился. Проверьте количество.");
          await read(shared.userID, shared.generation, true);
        } else if (status === 422) {
          lineError(detail || "Проверьте минимальное количество и шаг.");
        } else if (status !== undefined && status >= 400 && status < 500) {
          lineError("Действие не выполнено. Попробуйте ещё раз.", true);
        } else {
          // Network, timeout, 5xx, malformed or unexpected success: the write
          // may have committed. Never claim a rollback; reconcile read-only.
          message("Не удалось подтвердить изменение. Проверяем сохранённую корзину.");
          if (await read(shared.userID, shared.generation, true)) {
            message("Показана сохранённая корзина. Проверьте результат и при необходимости повторите действие.");
          }
        }
        return false;
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

    // Whole-cart scope: every authoritative line, independent of local search
    // or collapsed groups. An empty cart sends nothing.
    function selectAll(selected: boolean) {
      const productIDs = rt.state.snapshot?.cart.items.map((line) => line.product.id) ?? [];
      if (!productIDs.length) return Promise.resolve(false);
      return mutate({ kind: "selection", productIDs, selected });
    }

    return {
      sync,
      ensure: () => initialize(store.ensure()),
      refresh: () => initialize(store.refresh()),
      enterRoute: (route: CartRouteSession) => {
        void initialize(store.enterRoute(route));
        // The route itself presents anonymous/unavailable; no extra toast.
        if (route.status !== "authenticated") message("");
      },
      mutate,
      selectAll,
      signInRequired: () => message("Чтобы пользоваться корзиной, войдите в свой аккаунт."),
      dismissMessage: (text: string) => { if (rt.state.message === text) message(""); },
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

  // Settled confirmations fade so the toast never covers mobile inputs for long.
  useEffect(() => {
    if (!state.message || state.mutationPending || state.reconciling) return;
    const text = state.message;
    const timer = window.setTimeout(() => actions.dismissMessage(text), 6_000);
    return () => window.clearTimeout(timer);
  }, [actions, state.message, state.mutationPending, state.reconciling]);

  // Render-time ownership: a snapshot is visible only for the exact shared
  // session user and generation that read it.
  const owned = session.status === "authenticated" && state.snapshot?.ownerUserID === session.userID &&
    state.snapshot.generation === session.generation;
  const cart = state.status === "ready" && owned ? state.snapshot!.cart : null;
  return (
    <CartContext.Provider value={{ ...state, ...actions, cart, loginHref }}>
      {children}
      <div aria-atomic="true" aria-label="Уведомления корзины" aria-live="polite" className={styles.feedback} role="status">
        {state.message ? <span>{state.message}</span> : null}
      </div>
    </CartContext.Provider>
  );
}

export function useCart() {
  const value = useContext(CartContext);
  if (!value) throw new Error("Cart controls require CartProvider");
  return value;
}
