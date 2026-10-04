"use client";

import { usePathname } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { BrowserRequestError, ResponseOwnerError, type FieldError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Cart } from "@/lib/api/cart";
import { getBrowserCart, mutateBrowserCart, type CartCommand } from "@/lib/api/cart-client";
import { commitBrowserRepeat } from "@/lib/api/repeat-client";
import type { RepeatBody, RepeatReceipt } from "@/lib/api/repeat";
import type { Order } from "@/lib/api/order";
import { placeBrowserOrder, type CheckoutBody } from "@/lib/api/order-client";

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

// One logical checkout attempt, frozen by the /checkout view when submit
// starts. The same key and body are reused only for a deliberate check after
// an ambiguous outcome.
export type PlaceOrderAttempt = { userID: string; key: string; body: CheckoutBody };

// Definitive outcomes say whether an order was created; "unknown" never does.
export type PlaceOrderResult =
  | { kind: "placed"; order: Order; replayed: boolean }
  | { kind: "changed" | "outOfStock" | "conflict" }
  | { kind: "invalid"; fields: readonly FieldError[]; detail?: string }
  | { kind: "signedOut" | "csrf" | "ownerMismatch" | "idempotencyReused" }
  // Commit state cannot be known: only the same key and body may check it.
  | { kind: "unknown" }
  // The user changed: the attempt belongs to nobody current and is dropped.
  | { kind: "discarded" }
  // Another read/write is running or the reviewed snapshot is not current.
  | { kind: "blocked" };

export type RepeatAttempt = Readonly<{ userID: string; orderID: string; key: string; body: RepeatBody }>;
export type RepeatCommitResult =
  | { kind: "added"; receipt: RepeatReceipt; replayed: boolean }
  | { kind: "changed" | "outOfStock" | "notFound" | "signedOut" | "csrf" | "ownerMismatch" | "idempotencyReused" | "unknown" | "discarded" | "blocked" }
  | { kind: "invalid"; fields: readonly FieldError[]; detail?: string };
export type RepeatHandoff = { userID: string; generation: number; receipt: RepeatReceipt; fromPath: string };
type PlacedOrder = { userID: string; orderID: string };
type Runtime = {
  state: State;
  initialized: boolean;
  alive: boolean;
  generation: number;
  seenSession: string;
  read: AbortController | null;
  reading: Promise<void>;
  write: WriteJob | null;
  // In-memory confirmation handoff for /account/orders/[id]; never persisted.
  placedOrder: PlacedOrder | null;
  repeatReview: number;
  repeatHandoff: RepeatHandoff | null;
  pathname: string;
};

type CartContextValue = State & {
  loginHref: string | null;
  cart: Cart | null;
  ensure: () => Promise<void>;
  refresh: () => Promise<void>;
  enterRoute: (session: CartRouteSession) => void;
  mutate: (command: CartCommand) => Promise<boolean>;
  selectAll: (selected: boolean) => Promise<boolean>;
  placeOrder: (attempt: PlaceOrderAttempt, mode: "submit" | "check") => Promise<PlaceOrderResult>;
  beginRepeatReview: () => () => void;
  commitRepeat: (attempt: RepeatAttempt) => Promise<RepeatCommitResult>;
  enterCartRoute: (session: CartRouteSession) => RepeatHandoff | null;
  takePlacedOrder: (userID: string, orderID: string) => boolean;
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
  const pathname = usePathname();
  const [state, setState] = useState<State>(initial);
  const runtime = useRef<Runtime>({
    state: initial, initialized: false, alive: true, generation: 0, seenSession: "",
    read: null, reading: Promise.resolve(), write: null, placedOrder: null, repeatReview: 0, repeatHandoff: null, pathname: "",
  });

  useEffect(() => {
    const rt = runtime.current;
    rt.pathname = pathname;
    if (rt.repeatHandoff && pathname !== "/cart" && pathname !== rt.repeatHandoff.fromPath) rt.repeatHandoff = null;
  }, [pathname]);

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
      const shared = store.get();
      if (rt.repeatHandoff && (shared.status !== "authenticated" || shared.userID !== rt.repeatHandoff.userID || shared.generation !== rt.repeatHandoff.generation)) rt.repeatHandoff = null;
      if (!rt.initialized) return;
      const key = `${shared.generation}:${shared.status}`;
      if (rt.seenSession === key) return;
      rt.seenSession = key;
      rt.generation++;
      // A handoff belongs to one user; a settled other identity drops it. The
      // transient loading state of a revalidation keeps it.
      if (rt.placedOrder && (shared.status === "anonymous" || shared.status === "unavailable" ||
          (shared.status === "authenticated" && shared.userID !== rt.placedOrder.userID))) {
        rt.placedOrder = null;
      }
      rt.read?.abort();
      rt.read = null;
      publish({
        ...initial, status: shared.status === "authenticated" ? "loading" : shared.status,
        mutationPending: Boolean(rt.write), message: rt.state.message,
      });
      if (shared.status === "authenticated" && !rt.repeatReview) rt.reading = read(shared.userID, shared.generation).then(() => {});
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

    // Creating an order consumes the persisted Cart, so it shares the global
    // write lock. Its recovery differs from Cart writes: an order POST is not an
    // idempotent assignment, a Cart GET never proves whether an order exists,
    // and nothing here is ever resent. Only definitive outcomes reread the Cart.
    async function placeOrder(attempt: PlaceOrderAttempt, mode: "submit" | "check"): Promise<PlaceOrderResult> {
      const shared = store.get();
      const snapshot = rt.state.snapshot;
      if (!rt.alive || rt.write || rt.read || shared.status !== "authenticated" || shared.userID !== attempt.userID) {
        return { kind: "blocked" };
      }
      // A first submit confirms the visible review, which must be current. A
      // check resends a frozen body and does not depend on the Cart snapshot.
      if (mode === "submit" && (rt.state.status !== "ready" || rt.state.reconciling ||
          snapshot?.ownerUserID !== shared.userID || snapshot.generation !== shared.generation)) {
        return { kind: "blocked" };
      }
      const generation = rt.generation;
      const { userID, generation: sessionGeneration, csrfToken } = shared;
      let networkFinished!: () => void;
      const job: WriteJob = {
        userID, networkDone: new Promise<void>((resolve) => { networkFinished = resolve; }),
      };
      rt.write = job;
      publish({ ...rt.state, mutationPending: true, pendingProductID: null });
      // After the response: is the identity that sent it still the current one?
      async function settled(): Promise<"current" | "sameUser" | "other"> {
        if (store.get().status === "loading") await store.ensure();
        if (current(generation, sessionGeneration, userID)) return "current";
        const now = store.get();
        return rt.alive && now.status === "authenticated" && now.userID === userID ? "sameUser" : "other";
      }
      try {
        let placed: { order: Order; replayed: boolean } | null = null;
        let failure: unknown = null;
        try {
          placed = await placeBrowserOrder(attempt.body, attempt.key, csrfToken, { expectedUserID: userID })
            .finally(networkFinished);
        } catch (error) { failure = error; }
        const status = failure instanceof BrowserRequestError ? failure.status : undefined;
        const code = failure instanceof BrowserRequestError ? failure.code : undefined;
        // Another account answered (or Go refused the expected-user precondition
        // before any effect): never read it, never replay, recover the session.
        if (failure instanceof ResponseOwnerError || (status === 409 && code === "authenticated_user_changed")) {
          await ownerMismatch(sessionGeneration);
          return { kind: "ownerMismatch" };
        }
        const identity = await settled();
        if (identity === "other") return { kind: "discarded" };
        // Never publish across session generations; the same key can check.
        if (identity === "sameUser") return { kind: "unknown" };
        if (placed) {
          // The pre-order snapshot is stale. Hide it and let the next session
          // sync or route entry read the Cart once; never fabricate lines.
          rt.placedOrder = { userID, orderID: placed.order.id };
          rt.generation++;
          rt.seenSession = "";
          publish({ ...rt.state, status: "loading", snapshot: null, reconciling: false, errors: {}, message: "" });
          return { kind: "placed", ...placed };
        }
        if (status === 401) {
          signedOut();
          return { kind: "signedOut" };
        }
        if (status === 403 && code === "csrf_failed") {
          // Only reads follow; the order is never resent automatically.
          await initialize(store.refresh());
          return { kind: "csrf" };
        }
        if (status === 409 && (code === "checkout_changed" || code === "out_of_stock" || code === "conflict")) {
          await read(userID, sessionGeneration, true);
          return { kind: code === "checkout_changed" ? "changed" : code === "out_of_stock" ? "outOfStock" : "conflict" };
        }
        if (status === 409 && code === "idempotency_key_reused") return { kind: "idempotencyReused" };
        if (status === 422 && failure instanceof BrowserRequestError) {
          return { kind: "invalid", fields: failure.fields, detail: failure.detail };
        }
        // Network, timeout, 5xx, malformed 201 or any unexpected answer: the
        // order may exist. No Cart read can tell, so none is made.
        return { kind: "unknown" };
      } finally {
        if (rt.write === job) {
          rt.write = null;
          publish({ ...rt.state, mutationPending: false, pendingProductID: null });
        }
      }
    }

    // Repeat review owns its reads. Even a previously initialized Cart must not
    // issue a GET on session changes as evidence for an uncertain Repeat write.
    function beginRepeatReview() {
      rt.repeatReview++;
      rt.read?.abort(); rt.read = null; rt.generation++;
      return () => { rt.repeatReview = Math.max(0, rt.repeatReview - 1); };
    }

    async function commitRepeat(attempt: RepeatAttempt): Promise<RepeatCommitResult> {
      const fromPath = rt.pathname;
      const shared = store.get();
      if (!rt.alive || rt.write || rt.read || shared.status !== "authenticated" || shared.userID !== attempt.userID) return { kind: "blocked" };
      const generation = rt.generation;
      let networkFinished!: () => void;
      const job: WriteJob = { userID: shared.userID, networkDone: new Promise<void>((resolve) => { networkFinished = resolve; }) };
      rt.write = job;
      rt.repeatHandoff = null;
      publish({ ...rt.state, mutationPending: true });
      try {
        let result: Awaited<ReturnType<typeof commitBrowserRepeat>> | null = null;
        let failure: unknown;
        try {
          result = await commitBrowserRepeat(attempt.orderID, attempt.body, attempt.key, shared.csrfToken, { expectedUserID: shared.userID }).finally(networkFinished);
        } catch (error) { failure = error; }
        const error = failure instanceof BrowserRequestError ? failure : null;
        if (failure instanceof ResponseOwnerError || error?.code === "authenticated_user_changed") {
          await store.recoverOwner(shared.generation);
          return { kind: "ownerMismatch" };
        }
        if (store.get().status === "loading") await store.ensure();
        const now = store.get();
        if (!rt.alive || now.status !== "authenticated" || now.userID !== attempt.userID) return { kind: "discarded" };
        if (!current(generation, shared.generation, shared.userID)) return { kind: "unknown" };
        if (result) {
          rt.initialized = true;
          rt.seenSession = `${shared.generation}:${shared.status}`;
          rt.repeatHandoff = rt.pathname === fromPath ? { userID: shared.userID, generation: shared.generation, receipt: result.receipt, fromPath } : null;
          publish({ ...initial, status: "ready", userID: shared.userID, mutationPending: true,
            snapshot: { ownerUserID: shared.userID, generation: shared.generation, cart: result.cart } });
          return { kind: "added", receipt: result.receipt, replayed: result.replayed };
        }
        if (error?.status === 401) { signedOut(); return { kind: "signedOut" }; }
        if (error?.status === 403 && error.code === "csrf_failed") { await store.refresh(); return { kind: "csrf" }; }
        if (error?.status === 404) return { kind: "notFound" };
        if (error?.status === 409) {
          if (error.code === "repeat_changed") return { kind: "changed" };
          if (error.code === "out_of_stock") return { kind: "outOfStock" };
          if (error.code === "idempotency_key_reused") return { kind: "idempotencyReused" };
        }
        if (error?.status === 422) return { kind: "invalid", fields: error.fields, detail: error.detail };
        return { kind: "unknown" };
      } finally {
        if (rt.write === job) { rt.write = null; publish({ ...rt.state, mutationPending: false }); }
      }
    }

    function enterCartRoute(route: CartRouteSession): RepeatHandoff | null {
      const handoff = rt.repeatHandoff;
      rt.repeatHandoff = null;
      const shared = store.get();
      if (handoff && route.status === "authenticated" && shared.status === "authenticated" &&
          route.userID === handoff.userID && shared.userID === handoff.userID && shared.generation === handoff.generation &&
          rt.state.status === "ready" && rt.state.snapshot?.generation === shared.generation) return handoff;
      void initialize(store.enterRoute(route));
      return null;
    }

    // Consumes the confirmation handoff for exactly this current user and order.
    function takePlacedOrder(userID: string, orderID: string): boolean {
      const handoff = rt.placedOrder;
      const shared = store.get();
      if (!handoff || shared.status !== "authenticated" || shared.userID !== userID ||
          handoff.userID !== userID || handoff.orderID !== orderID) return false;
      rt.placedOrder = null;
      return true;
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
      placeOrder,
      beginRepeatReview,
      commitRepeat,
      enterCartRoute,
      takePlacedOrder,
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
