import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/products/p01" }));

import { apiError, catalogOptions, json, mockApi, product, testStaff } from "@/test/api-mock";
import { archiveExplanation, conflictMessage, ProductEditor } from "./product-editor";
import { StaffSessionProvider } from "./staff-session";
import { unsavedChangesMessage } from "./use-unsaved-changes";

const options = json(catalogOptions);
const PRODUCT = "/api/admin/v1/products/p01";

function renderEditor(productId?: string) {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <ProductEditor productId={productId} />
    </StaffSessionProvider>,
  );
}

async function replace(label: string, value: string) {
  const user = userEvent.setup();
  const input = screen.getByLabelText(label);
  await user.clear(input);
  if (value) await user.type(input, value);
}

const noDeletes = (api: ReturnType<typeof mockApi>) => expect(api.calls.filter((call) => call.method === "DELETE")).toEqual([]);

describe("product editor", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe("create", () => {
    it("lists every missing field before calling Go and links to it", async () => {
      const api = mockApi({ "GET /api/admin/v1/catalog-options": options });
      renderEditor();
      await userEvent.setup().click(await screen.findByRole("button", { name: "Создать товар" }));
      const summary = await screen.findByRole("alert");
      expect(within(summary).getByRole("heading", { name: "Исправьте ошибки" })).toBeInTheDocument();
      expect(within(summary).getByRole("link", { name: /Название: Обязательное поле/ })).toHaveAttribute("href", "#product-name");
      expect(within(summary).getByRole("link", { name: /Цена, ₽: Укажите цену/ })).toBeInTheDocument();
      expect(screen.getByLabelText("Название")).toHaveAttribute("aria-invalid", "true");
      await waitFor(() => expect(summary).toHaveFocus());
      expect(api.called("POST /api/admin/v1/products")).toHaveLength(0);
    });

    it("keeps brands to the chosen manufacturer and never submits a foreign one", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options });
      renderEditor();
      const user = userEvent.setup();
      const brand = await screen.findByLabelText("Бренд");
      expect(brand).toBeDisabled();
      await user.selectOptions(screen.getByLabelText("Производитель"), "m-choco");
      expect(within(brand).getAllByRole("option").map((option) => option.textContent)).toEqual(["Выберите бренд", "Atelier Noir"]);
      await user.selectOptions(brand, "b-noir");
      await user.selectOptions(screen.getByLabelText("Производитель"), "m-caffarel");
      expect(brand).toHaveValue("");
      expect(within(screen.getByLabelText("Категория")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Выберите категорию", "Шоколад", "— Плитки"]);
    });

    it("sends the exact B1 document with CSRF and the expected staff ID, then opens the created product", async () => {
      const created = product({ id: "prd_" + "9".repeat(32), revision: 1 });
      const api = mockApi({ "GET /api/admin/v1/catalog-options": options, "POST /api/admin/v1/products": json(created, 201) });
      renderEditor();
      const user = userEvent.setup();
      await screen.findByLabelText("Название");
      await replace("Название", "Молочный шоколад");
      await replace("Slug (адрес страницы)", "milk-bar");
      await replace("Артикул", "MLK-1");
      await replace("Цена, ₽", "199,90");
      await replace("Старая цена, ₽", "249.9");
      await replace("Остаток", "12");
      await replace("Минимальное количество", "2");
      await replace("Шаг количества", "2");
      await replace("Единица продажи", "плитка 100 г");
      await user.selectOptions(screen.getByLabelText("Категория"), "cat-bars");
      await user.selectOptions(screen.getByLabelText("Производитель"), "m-choco");
      await user.selectOptions(screen.getByLabelText("Бренд"), "b-noir");
      await user.click(screen.getByLabelText("Подарки"));
      await user.type(screen.getByLabelText("Теги"), "орехи{enter}подарок");
      await user.click(screen.getByRole("button", { name: "Создать товар" }));
      await waitFor(() => expect(router.replace).toHaveBeenCalledWith(`/products/${created.id}?created=1`));
      const [call] = api.called("POST /api/admin/v1/products");
      expect(call.body).toEqual({
        slug: "milk-bar", sku: "MLK-1", name: "Молочный шоколад", shortName: "", description: "", priceMinor: 19990, oldPriceMinor: 24990,
        unit: "плитка 100 г", weightGrams: 0, categoryId: "cat-bars", manufacturerId: "m-choco", brandId: "b-noir", collectionIds: ["col-gifts"],
        series: "", tags: ["орехи", "подарок"], badges: [], ingredients: "", nutrition: "", shelfLifeDays: 0, stock: 12, minimumQuantity: 2,
        quantityStep: 2, featured: false,
      });
      expect(call.headers.get("X-CSRF-Token")).toBe("csrf-1");
      expect(call.headers.get("X-Expected-Staff-ID")).toBe(testStaff.id);
      noDeletes(api);
    });
  });

  describe("edit", () => {
    it("loads the admin product, saves with the loaded revision and adopts the server answer", async () => {
      const api = mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: json(product()),
        [`PUT ${PRODUCT}`]: json(product({ revision: 8, stock: 30, updatedAt: "2026-10-24T10:00:00Z" })),
      });
      renderEditor("p01");
      expect(await screen.findByLabelText("Цена, ₽")).toHaveValue("489,00");
      expect(screen.getByTestId("product-revision")).toHaveTextContent("7");
      expect(screen.getByText("/assets/product-chocolate.webp")).toBeInTheDocument();
      expect(screen.getByText(/управление изображениями будет добавлено отдельно/)).toBeInTheDocument();
      expect(document.querySelector("input[type=file]")).toBeNull();
      await replace("Остаток", "30");
      expect(screen.getByText("Есть несохранённые изменения")).toBeInTheDocument();
      await userEvent.setup().click(screen.getByRole("button", { name: "Сохранить изменения" }));
      expect(await screen.findByText("Изменения сохранены. Ревизия 8.")).toBeInTheDocument();
      const [call] = api.called(`PUT ${PRODUCT}`);
      expect(call.body).toMatchObject({ expectedRevision: 7, stock: 30, priceMinor: 48900 });
      expect(screen.getByTestId("product-revision")).toHaveTextContent("8");
      expect(screen.queryByText("Есть несохранённые изменения")).toBeNull();
      expect(api.calls.some((c) => c.url.startsWith("/api/") && !c.url.startsWith("/api/admin/"))).toBe(false);
    });

    it("shows Go's field errors on their controls with a summary", async () => {
      mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: json(product()),
        [`PUT ${PRODUCT}`]: apiError(422, "validation_error", [
          { field: "priceMinor", message: "Цена в копейках: от 1 до 1000000000" },
          { field: "slug", message: "Slug уже используется другим товаром" },
        ]),
      });
      renderEditor("p01");
      await screen.findByLabelText("Цена, ₽");
      await userEvent.setup().click(screen.getByRole("button", { name: "Сохранить изменения" }));
      const summary = await screen.findByRole("alert");
      expect(summary).toHaveTextContent("Slug уже используется другим товаром");
      expect(screen.getByLabelText("Цена, ₽")).toHaveAttribute("aria-invalid", "true");
      expect(screen.getByLabelText("Цена, ₽")).toHaveAccessibleDescription(/Цена в копейках/);
      expect(screen.getByLabelText("Slug (адрес страницы)")).toHaveAttribute("aria-invalid", "true");
    });

    it("on a stale revision keeps local edits, never retries, and adopts the new revision only after an explicit reload", async () => {
      const api = mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: [json(product()), json(product({ revision: 9, stock: 5, name: "Изменено коллегой" }))],
        [`PUT ${PRODUCT}`]: [apiError(409, "stale_revision"), json(product({ revision: 10 }))],
      });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      await replace("Остаток", "99");
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
      const conflict = await screen.findByText(conflictMessage);
      expect(conflict.closest("[role=alert]")).not.toBeNull();
      expect(screen.getByLabelText("Остаток")).toHaveValue("99");
      expect(screen.getByTestId("product-revision")).toHaveTextContent("7");
      expect(screen.getByRole("button", { name: "Сохранить изменения" })).toBeDisabled();
      expect(api.called(`PUT ${PRODUCT}`)).toHaveLength(1);
      expect(api.called(`GET ${PRODUCT}`)).toHaveLength(1);

      await user.click(screen.getByRole("button", { name: "Загрузить актуальную версию" }));
      expect(await screen.findByText("Загружена актуальная версия. Ревизия 9.")).toBeInTheDocument();
      expect(screen.queryByText(conflictMessage)).toBeNull();
      expect(screen.getByLabelText("Остаток")).toHaveValue("5");
      expect(screen.getByLabelText("Название")).toHaveValue("Изменено коллегой");
      await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
      await waitFor(() => expect(api.called(`PUT ${PRODUCT}`)).toHaveLength(2));
      expect(api.called(`PUT ${PRODUCT}`).map((call) => (call.body as { expectedRevision: number }).expectedRevision)).toEqual([7, 9]);
    });

    it("lets the operator drop local edits from the conflict state", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()), [`PUT ${PRODUCT}`]: apiError(409, "stale_revision") });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      await replace("Остаток", "99");
      const user = userEvent.setup();
      await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
      await user.click(await screen.findByRole("button", { name: "Отменить мои изменения и вернуться к списку" }));
      expect(router.push).toHaveBeenCalledWith("/products");
    });

    it("refreshes a rejected CSRF token once, and only once", async () => {
      const api = mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: json(product()),
        "GET /api/admin/v1/auth/session": [
          json({ authenticated: true, staff: { ...testStaff, createdAt: "x" }, expiresAt: "x", csrfToken: "old" }),
          json({ authenticated: true, staff: { ...testStaff, createdAt: "x" }, expiresAt: "x", csrfToken: "new" }),
        ],
        [`PUT ${PRODUCT}`]: apiError(403, "csrf_failed"),
      });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      await waitFor(() => expect(api.called("GET /api/admin/v1/auth/session")).toHaveLength(1));
      await userEvent.setup().click(screen.getByRole("button", { name: "Сохранить изменения" }));
      expect(await screen.findByText(/Не удалось подтвердить запрос/)).toBeInTheDocument();
      expect(api.called(`PUT ${PRODUCT}`).map((call) => call.headers.get("X-CSRF-Token"))).toEqual(["old", "new"]);
    });

    it("hands an expired session to sign-in without claiming success", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()), [`PUT ${PRODUCT}`]: apiError(401, "unauthorized") });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      await userEvent.setup().click(screen.getByRole("button", { name: "Сохранить изменения" }));
      await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
      expect(screen.queryByText(/Изменения сохранены/)).toBeNull();
    });

    it("says when the product does not exist", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options, "GET /api/admin/v1/products/missing": apiError(404, "not_found") });
      renderEditor("missing");
      expect(await screen.findByRole("heading", { name: "Товар не найден" })).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "К списку товаров" })).toHaveAttribute("href", "/products");
    });

    it("warns before leaving with unsaved changes, and only then", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()) });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      const unload = () => {
        const event = new Event("beforeunload", { cancelable: true });
        window.dispatchEvent(event);
        return event.defaultPrevented;
      };
      expect(unload()).toBe(false);
      await replace("Остаток", "1");
      expect(unload()).toBe(true);
      const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
      const link = screen.getByRole("link", { name: "Товары" });
      const click = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
      link.dispatchEvent(click);
      expect(confirm).toHaveBeenCalledWith(unsavedChangesMessage);
      expect(click.defaultPrevented).toBe(true);
      await replace("Остаток", "24");
      expect(unload()).toBe(false);
    });
  });

  describe("archive and restore", () => {
    it("asks for confirmation, sends the current revision and shows the archived state", async () => {
      const api = mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: json(product()),
        [`POST ${PRODUCT}/archive`]: json(product({ archived: true, archivedAt: "2026-10-24T10:00:00Z", revision: 8 })),
      });
      renderEditor("p01");
      const user = userEvent.setup();
      await user.click(await screen.findByRole("button", { name: "Снять с продажи…" }));
      expect(api.called(`POST ${PRODUCT}/archive`)).toHaveLength(0);
      const dialog = screen.getByRole("group", { name: "Перенести товар в архив?" });
      expect(dialog).toHaveTextContent(archiveExplanation);
      expect(archiveExplanation).toMatch(/Заказы, корзины и избранное покупателей сохранятся/);
      await user.click(within(dialog).getByRole("button", { name: "Отмена" }));
      expect(screen.queryByRole("group", { name: "Перенести товар в архив?" })).toBeNull();
      await user.click(screen.getByRole("button", { name: "Снять с продажи…" }));
      await user.click(screen.getByRole("button", { name: "Перенести в архив" }));
      expect(await screen.findByText("Товар перенесён в архив. Ревизия 8.")).toBeInTheDocument();
      expect(api.called(`POST ${PRODUCT}/archive`)[0].body).toEqual({ expectedRevision: 7 });
      expect(screen.getAllByText("В архиве").length).toBeGreaterThan(0);
      expect(screen.getByRole("button", { name: "Вернуть в продажу" })).toBeEnabled();
      expect(screen.queryByRole("button", { name: "Снять с продажи…" })).toBeNull();
      noDeletes(api);
    });

    it("treats a stale archive like a stale save", async () => {
      const api = mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()), [`POST ${PRODUCT}/archive`]: apiError(409, "stale_revision") });
      renderEditor("p01");
      const user = userEvent.setup();
      await user.click(await screen.findByRole("button", { name: "Снять с продажи…" }));
      await user.click(screen.getByRole("button", { name: "Перенести в архив" }));
      expect(await screen.findByText(conflictMessage)).toBeInTheDocument();
      expect(screen.getByTestId("product-revision")).toHaveTextContent("7");
      expect(api.called(`POST ${PRODUCT}/archive`)).toHaveLength(1);
    });

    it("restores an archived product with its current revision", async () => {
      const api = mockApi({
        "GET /api/admin/v1/catalog-options": options,
        [`GET ${PRODUCT}`]: json(product({ archived: true, revision: 8 })),
        [`POST ${PRODUCT}/restore`]: json(product({ revision: 9 })),
      });
      renderEditor("p01");
      expect(await screen.findByText(/Товар в архиве: покупатели его не видят/)).toBeInTheDocument();
      await userEvent.setup().click(screen.getByRole("button", { name: "Вернуть в продажу" }));
      expect(await screen.findByText("Товар снова в продаже. Ревизия 9.")).toBeInTheDocument();
      expect(api.called(`POST ${PRODUCT}/restore`)[0].body).toEqual({ expectedRevision: 8 });
      expect(screen.getByRole("button", { name: "Снять с продажи…" })).toBeEnabled();
      noDeletes(api);
    });

    it("does not archive over unsaved edits", async () => {
      mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()) });
      renderEditor("p01");
      await screen.findByLabelText("Остаток");
      await replace("Остаток", "1");
      expect(screen.getByRole("button", { name: "Снять с продажи…" })).toBeDisabled();
      expect(screen.getByText("Сначала сохраните или отмените изменения формы.")).toBeInTheDocument();
    });
  });

  it("keeps product data and credentials out of browser storage", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    mockApi({ "GET /api/admin/v1/catalog-options": options, [`GET ${PRODUCT}`]: json(product()), [`PUT ${PRODUCT}`]: json(product({ revision: 8 })) });
    renderEditor("p01");
    await screen.findByLabelText("Остаток");
    await userEvent.setup().click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await screen.findByText("Изменения сохранены. Ревизия 8.");
    expect(setItem).not.toHaveBeenCalled();
    expect(document.body.innerHTML).not.toContain("csrf-1");
  });
});
