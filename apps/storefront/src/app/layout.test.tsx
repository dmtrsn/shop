import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({ usePathname: () => "/", useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));

import RootLayout, { viewport } from "./layout";

describe("root layout shell", () => {
  it("renders skip link, header, the page, footer and tab bar in reading order", () => {
    const html = renderToStaticMarkup(<RootLayout><main id="main-content">Страница</main></RootLayout>);
    const order = ["Перейти к содержимому", "<header", "Страница", "<footer", 'aria-label="Основная навигация"'].map((marker) => html.indexOf(marker));
    expect(order.every((index) => index >= 0)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
    expect(html.match(/<main/g)).toHaveLength(1);
  });

  it("opts into safe-area insets for the fixed tab bar", () => {
    expect(viewport.viewportFit).toBe("cover");
  });
});
