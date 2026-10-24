import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/dictionaries/categories/cat-new" }));

import { apiError, brand, category, collection, json, manufacturer, mockApi, testStaff } from "@/test/api-mock";
import type { DictionaryKind } from "@/lib/dictionaries-api";
import { archiveExplanations, DictionaryEditor, dictionaryConflictMessage, referenceConflictText } from "./dictionary-editor";
import { StaffSessionProvider } from "./staff-session";

const CATEGORIES = "/api/admin/v1/categories";
const MANUFACTURERS = "/api/admin/v1/manufacturers";

function renderEditor(kind: DictionaryKind, entryId?: string) {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <DictionaryEditor entryId={entryId} kind={kind} />
    </StaffSessionProvider>,
  );
}

const tree = json({
  items: [
    category(),
    category({ id: "cat-bars", slug: "bars", name: "Плитки", parentId: "cat-choc" }),
    category({ id: "cat-old", slug: "old", name: "Старое", archived: true }),
    category({ id: "cat-candy", slug: "candy", name: "Конфеты" }),
  ],
});

const noDeletes = (api: ReturnType<typeof mockApi>) => expect(api.calls.filter((call) => call.method === "DELETE")).toEqual([]);

describe("dictionary editor", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("creates a subcategory under an active top-level category only", async () => {
    const api = mockApi({ [`GET ${CATEGORIES}`]: tree, [`POST ${CATEGORIES}`]: json(category({ id: "cat_" + "1".repeat(32) }), 201) });
    renderEditor("categories");
    const user = userEvent.setup();
    const parent = await screen.findByLabelText("Родительская категория");
    // Subcategories cannot be parents (two levels), and archived entries are not offered anew.
    expect(within(parent).getAllByRole("option").map((option) => option.textContent)).toEqual(["Нет — корневая категория", "Шоколад", "Конфеты"]);
    await user.type(screen.getByLabelText("Название"), "Джемы");
    await user.type(screen.getByLabelText("Slug (адрес в каталоге)"), "jams");
    await user.selectOptions(parent, "cat-candy");
    await user.click(screen.getByRole("button", { name: "Создать" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith(`/dictionaries/categories/cat_${"1".repeat(32)}?created=1`));
    const [create] = api.called(`POST ${CATEGORIES}`);
    expect(create.body).toEqual({ slug: "jams", name: "Джемы", description: "", parentId: "cat-candy", icon: "", sortOrder: 0 });
    expect(create.headers.get("X-CSRF-Token")).toBe("csrf-1");
    expect(create.headers.get("X-Expected-Staff-ID")).toBe(testStaff.id);
  });

  it("keeps a category with subcategories top-level and its archived parent visible", async () => {
    mockApi({ [`GET ${CATEGORIES}`]: tree, [`GET ${CATEGORIES}/cat-choc`]: json(category({ childCount: 1 })) });
    renderEditor("categories", "cat-choc");
    const parent = await screen.findByLabelText("Родительская категория");
    expect(parent).toBeDisabled();
    expect(screen.getByText("У категории есть подкатегории, поэтому она остаётся корневой")).toBeInTheDocument();
    // The category itself is never its own parent option.
    expect(within(parent).queryByRole("option", { name: "Шоколад" })).toBeNull();
  });

  it("labels the current archived parent instead of dropping it", async () => {
    mockApi({ [`GET ${CATEGORIES}`]: tree, [`GET ${CATEGORIES}/cat-x`]: json(category({ id: "cat-x", slug: "x", name: "Икс", parentId: "cat-old", archived: true })) });
    renderEditor("categories", "cat-x");
    const parent = await screen.findByLabelText("Родительская категория");
    expect(parent).toHaveValue("cat-old");
    expect(within(parent).getByRole("option", { name: "Старое (в архиве)" })).toBeInTheDocument();
  });

  it("shows a tree refusal next to the parent", async () => {
    mockApi({ [`GET ${CATEGORIES}`]: tree, [`GET ${CATEGORIES}/cat-candy`]: json(category({ id: "cat-candy", slug: "candy", name: "Конфеты" })), [`PUT ${CATEGORIES}/cat-candy`]: apiError(422, "invalid_hierarchy") });
    renderEditor("categories", "cat-candy");
    const user = userEvent.setup();
    await user.selectOptions(await screen.findByLabelText("Родительская категория"), "cat-choc");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    const summary = await screen.findByRole("alert");
    expect(within(summary).getByRole("link", { name: /Родительская категория: Категории образуют два уровня/ })).toHaveAttribute("href", "#dictionary-parentId");
  });

  it("on a stale revision keeps the form, never retries, and adopts the server version only on reload", async () => {
    const api = mockApi({
      [`GET ${MANUFACTURERS}/m-choco`]: [json(manufacturer({ revision: 3 })), json(manufacturer({ revision: 4, name: "Choco Atelier SPb" }))],
      [`PUT ${MANUFACTURERS}/m-choco`]: apiError(409, "stale_revision"),
    });
    renderEditor("manufacturers", "m-choco");
    const user = userEvent.setup();
    const name = await screen.findByLabelText("Название");
    await user.clear(name);
    await user.type(name, "Моё название");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    expect(await screen.findByText(dictionaryConflictMessage)).toBeInTheDocument();
    expect(api.called(`PUT ${MANUFACTURERS}/m-choco`)).toHaveLength(1);
    expect(api.called(`PUT ${MANUFACTURERS}/m-choco`)[0].body).toMatchObject({ expectedRevision: 3, name: "Моё название" });
    expect(screen.getByLabelText("Название")).toHaveValue("Моё название");
    expect(screen.getByRole("button", { name: "Сохранить изменения" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Загрузить актуальную версию" }));
    expect(await screen.findByText("Загружена актуальная версия. Ревизия 4.")).toBeInTheDocument();
    expect(screen.getByLabelText("Название")).toHaveValue("Choco Atelier SPb");
    expect(screen.getByTestId("dictionary-revision")).toHaveTextContent("4");
  });

  it("explains why a used entry cannot be archived and changes nothing", async () => {
    const api = mockApi({
      [`GET ${MANUFACTURERS}/m-choco`]: json(manufacturer({ productCount: 5 })),
      [`POST ${MANUFACTURERS}/m-choco/archive`]: () => Response.json({ error: { code: "reference_conflict", message: "x", requestId: "r", references: { products: 5, brands: 2, categories: 0 } } }, { status: 409 }),
    });
    renderEditor("manufacturers", "m-choco");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Перенести в архив…" }));
    const dialog = screen.getByRole("group", { name: "Перенести в архив?" });
    expect(dialog).toHaveTextContent(archiveExplanations.manufacturers);
    await user.click(within(dialog).getByRole("button", { name: "Перенести в архив" }));
    const alert = await screen.findByText(referenceConflictText({ products: 5, brands: 2, categories: 0 }));
    expect(alert).toHaveTextContent("товаров — 5, активных брендов — 2");
    expect(api.called(`POST ${MANUFACTURERS}/m-choco/archive`)[0].body).toEqual({ expectedRevision: 1 });
    expect(screen.queryByText(dictionaryConflictMessage)).toBeNull();
    expect(screen.getByText("Активен")).toBeInTheDocument();
    noDeletes(api);
  });

  it("archives and restores a collection, keeping its products", async () => {
    const api = mockApi({
      [`GET /api/admin/v1/collections/col-gifts`]: json(collection({ productCount: 5 })),
      [`POST /api/admin/v1/collections/col-gifts/archive`]: json(collection({ revision: 2, archived: true, archivedAt: "2026-10-24T10:00:00Z", productCount: 5 })),
      [`POST /api/admin/v1/collections/col-gifts/restore`]: json(collection({ revision: 3, productCount: 5 })),
    });
    renderEditor("collections", "col-gifts");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Перенести в архив…" }));
    expect(archiveExplanations.collections).toMatch(/Товары останутся в ней/);
    await user.click(screen.getByRole("button", { name: "Перенести в архив" }));
    expect(await screen.findByText("Запись перенесена в архив. Ревизия 2.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Восстановить" }));
    expect(await screen.findByText("Запись восстановлена. Ревизия 3.")).toBeInTheDocument();
    expect(api.called(`POST /api/admin/v1/collections/col-gifts/restore`)[0].body).toEqual({ expectedRevision: 2 });
    noDeletes(api);
  });

  it("says why a brand cannot be restored under an archived manufacturer", async () => {
    mockApi({
      [`GET ${MANUFACTURERS}`]: json({ items: [manufacturer({ archived: true })] }),
      [`GET /api/admin/v1/brands/b-noir`]: json(brand({ archived: true })),
      [`POST /api/admin/v1/brands/b-noir/restore`]: apiError(422, "validation_error", [{ field: "manufacturerId", message: "Сначала восстановите производителя" }]),
    });
    renderEditor("brands", "b-noir");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Восстановить" }));
    expect(await screen.findByText("Сначала восстановите производителя")).toBeInTheDocument();
  });

  it("locks the manufacturer of a brand that products use", async () => {
    mockApi({
      [`GET ${MANUFACTURERS}`]: json({ items: [manufacturer(), manufacturer({ id: "m-old", name: "Старый", archived: true })] }),
      [`GET /api/admin/v1/brands/b-noir`]: json(brand({ productCount: 3 })),
    });
    renderEditor("brands", "b-noir");
    const select = await screen.findByLabelText("Производитель");
    expect(select).toBeDisabled();
    expect(within(select).queryByRole("option", { name: /Старый/ })).toBeNull();
    expect(screen.getByText("Бренд используется в товарах (3), поэтому производителя сменить нельзя")).toBeInTheDocument();
  });

  it("says when the entry does not exist", async () => {
    mockApi({ [`GET ${MANUFACTURERS}/missing`]: apiError(404, "not_found") });
    renderEditor("manufacturers", "missing");
    expect(await screen.findByRole("heading", { name: "Запись не найдена" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "К списку: производители" })).toHaveAttribute("href", "/dictionaries/manufacturers");
  });
});
