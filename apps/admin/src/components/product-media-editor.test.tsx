import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
const router = { replace: vi.fn(), refresh: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/products/p01" }));
import { apiError, catalogOptions, json, mockApi, product, testStaff } from "@/test/api-mock";
import { ProductEditor } from "./product-editor";
import { StaffSessionProvider } from "./staff-session";

const asset = (letter: string) => ({ id: `med_${letter.repeat(32)}`, url: `/media/med_${letter.repeat(32)}`, width: 40, height: 30, contentType: "image/jpeg", position: 0, productCount: 0, orderCount: 0 });
const a = asset("a"), b = asset("b"), c = asset("c");
const path = "/api/admin/v1/products/p01";
function setup(routes: Parameters<typeof mockApi>[0] = {}, managed = false) {
  const api = mockApi({
    "GET /api/admin/v1/catalog-options": json(catalogOptions),
    [`GET ${path}`]: json(product(managed ? { managedMedia: { primary: a, gallery: [b,c] } } : {})),
    "GET /api/admin/v1/media": json({ items: [a,b,c], page: 1, totalPages: 1 }),
    ...routes,
  });
  render(<StaffSessionProvider staff={testStaff}><ProductEditor productId="p01" /></StaffSessionProvider>);
  return api;
}
async function candidate(id: string) {
  const list = await screen.findByRole("list", { name: "Доступные изображения" });
  await waitFor(() => expect(list.querySelector(`[data-media-id="${id}"]`)).not.toBeNull());
  return within(list.querySelector(`[data-media-id="${id}"]`) as HTMLElement);
}
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("product media editor", () => {
  it("uploads multipart with staff guards, previews, selects primary/gallery, reorders and atomically saves", async () => {
    const uploaded = asset("d");
    const api = setup({ "POST /api/admin/v1/media": json(uploaded,201), [`PUT ${path}/media`]: json({ revision: 8 }) });
    const user = userEvent.setup();
    await user.upload(await screen.findByLabelText(/Загрузить изображение/), new File(["image bytes"], "photo.png", { type: "image/png" }));
    const row = await candidate(uploaded.id);
    expect(row.getByRole("img")).toHaveAttribute("src", uploaded.url);
    const call = api.called("POST /api/admin/v1/media")[0];
    expect(call.body).toBeInstanceOf(FormData);
    expect(call.headers.has("Content-Type")).toBe(false);
    expect(call.headers.get("X-Expected-Staff-ID")).toBe(testStaff.id);
    expect(call.headers.get("X-CSRF-Token")).toBe("csrf-1");
    await user.click(row.getByRole("button", { name: "Основное" }));
    await user.click((await candidate(b.id)).getByRole("button", { name: "В галерею" }));
    await user.click((await candidate(c.id)).getByRole("button", { name: "В галерею" }));
    await user.click(screen.getByRole("button", { name: "Фото 2: вверх" }));
    expect(screen.getByRole("button", { name: "Сохранить изменения" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Сохранить изображения" }));
    await screen.findByText("Изображения сохранены. Ревизия 8.");
    expect(api.called(`PUT ${path}/media`)[0].body).toEqual({ expectedRevision: 7, primaryId: uploaded.id, galleryIds: [c.id,b.id] });
  });

  it("preserves selections on save failure and explicitly reloads after a stale revision", async () => {
    const api = setup({
      [`PUT ${path}/media`]: [apiError(503,"storage_unavailable"), apiError(409,"stale_revision")],
      [`GET ${path}`]: [json(product()), json(product({ revision: 9, managedMedia: { primary: b, gallery: [] } }))],
    });
    const user=userEvent.setup();
    await user.click((await candidate(a.id)).getByRole("button", { name: "Основное" }));
    await user.click(screen.getByRole("button", { name: "Сохранить изображения" }));
    await screen.findByText(/Хранилище изображений недоступно/);
    expect(within(screen.getByTestId("media-primary")).getByRole("img")).toHaveAttribute("src",a.url);
    await user.click(screen.getByRole("button", { name: "Сохранить изображения" }));
    await screen.findByText("Товар был изменён другим сотрудником. Ваши изменения не сохранены.");
    expect(api.called(`PUT ${path}/media`)).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Загрузить актуальную версию" }));
    await screen.findByText("Загружена актуальная версия. Ревизия 9.");
    expect(within(screen.getByTestId("media-primary")).getByRole("img")).toHaveAttribute("src",b.url);
  });

  it("detaches without deleting, then explains reference conflicts for explicit global deletion", async () => {
    const api=setup({ [`DELETE /api/admin/v1/media/${b.id}`]: json({ error: { code: "reference_conflict", references: { products: 2, orders: 1 } } },409) },true);
    const user=userEvent.setup();
    await user.click(await screen.findByRole("button",{name:"Убрать фото 1 из товара"}));
    expect(api.calls.filter((call)=>call.method==="DELETE")).toHaveLength(0);
    await user.click((await candidate(b.id)).getByRole("button",{name:"Удалить файл…"}));
    await user.click(screen.getByRole("button",{name:"Удалить файл навсегда"}));
    await screen.findByText(/Удаление заблокировано.*товаров — 2.*заказов — 1/);
    expect(within(screen.getByRole("list",{name:"Галерея товара"})).getAllByRole("img")).toHaveLength(1);
    expect(screen.getByText("Есть несохранённые изменения изображений.")).toBeInTheDocument();
  });

  it.each([
    ["products only",{products:2,brands:0,categories:0},"Удаление заблокировано: файл используется в товарах (товаров — 2). Сначала отсоедините его от этих товаров и сохраните изменения."],
    ["historical orders only",{products:0,brands:0,categories:0,orders:3},"Удаление заблокировано: файл хранится в истории заказов (заказов — 3), поэтому его нельзя удалить."],
    ["products and historical orders",{products:2,brands:0,categories:0,orders:3},"Удаление заблокировано: файл используется в товарах (товаров — 2) и хранится в истории заказов (заказов — 3). Отсоединение от товаров не снимет запрет: файл из истории заказов удалить нельзя."],
  ])("explains a delete conflict from %s using counts alone",async(_case,references,message)=>{
    setup({[`DELETE /api/admin/v1/media/${c.id}`]:json({error:{code:"reference_conflict",references}},409)});
    const user=userEvent.setup();
    await user.click((await candidate(c.id)).getByRole("button",{name:"Удалить файл…"}));
    await user.click(screen.getByRole("button",{name:"Удалить файл навсегда"}));
    expect(await screen.findByRole("alert")).toHaveTextContent(message);
  });

  it("deletes an unused asset only after confirmation",async()=>{
    const api=setup({ [`DELETE /api/admin/v1/media/${c.id}`]: ()=>new Response(null,{status:204}), "GET /api/admin/v1/media": [json({items:[a,b,c],page:1,totalPages:1}),json({items:[a,b],page:1,totalPages:1})] });
    const user=userEvent.setup();
    await user.click((await candidate(c.id)).getByRole("button",{name:"Удалить файл…"}));
    expect(api.calls.filter((call)=>call.method==="DELETE")).toHaveLength(0);
    await user.click(screen.getByRole("button",{name:"Удалить файл навсегда"}));
    await waitFor(()=>expect(screen.getByRole("list",{name:"Доступные изображения"}).querySelector(`[data-media-id="${c.id}"]`)).toBeNull());
  });

  it.each([
    ["media_too_large",413,"Файл больше 10 МБ"],
    ["unsupported_media_type",415,"Поддерживаются JPEG, PNG и WebP"],
    ["storage_unavailable",503,"Хранилище изображений недоступно"],
    ["unauthorized",401,"Сеанс завершён"],
  ])("explains a %s upload refusal without touching the selection",async(code,status,message)=>{
    setup({"POST /api/admin/v1/media":apiError(status,code)},true);
    await userEvent.setup().upload(await screen.findByLabelText(/Загрузить изображение/),new File(["x"],"photo.png",{type:"image/png"}));
    await screen.findByText(new RegExp(message));
    expect(within(screen.getByTestId("media-primary")).getByRole("img")).toHaveAttribute("src",a.url);
  });

  it("refuses oversized and unsupported files before uploading",async()=>{
    const api=setup({},true);
    const user=userEvent.setup({applyAccept:false});
    const input=await screen.findByLabelText(/Загрузить изображение/);
    const large=new File(["x"],"large.png",{type:"image/png"});
    Object.defineProperty(large,"size",{value:10*1024*1024+1});
    await user.upload(input,large);
    await screen.findByText(/Файл больше 10 МБ/);
    await user.upload(input,new File(["x"],"anim.gif",{type:"image/gif"}));
    await screen.findByText(/Поддерживаются JPEG, PNG и WebP/);
    expect(api.called("POST /api/admin/v1/media")).toHaveLength(0);
  });

  it("reports upload validation failures and keeps the current primary",async()=>{
    setup({"POST /api/admin/v1/media":apiError(422,"validation_error",[{field:"file",message:"Файл изображения повреждён или обрезан"}])},true);
    const user=userEvent.setup();
    await user.upload(await screen.findByLabelText(/Загрузить изображение/),new File(["bad"],"bad.png",{type:"image/png"}));
    await screen.findByText("Файл изображения повреждён или обрезан");
    expect(within(screen.getByTestId("media-primary")).getByRole("img")).toHaveAttribute("src",a.url);
  });
});
