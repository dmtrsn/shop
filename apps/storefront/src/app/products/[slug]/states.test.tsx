import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import ProductError from "./error";
import ProductNotFound from "./not-found";

describe("product route states", () => {
  it("renders a useful product not-found state", () => {
    render(<ProductNotFound />);

    expect(screen.getByRole("heading", { name: "Товар не найден" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "На главную" })).toHaveAttribute("href", "/");
  });

  it("renders a recoverable backend error state", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<ProductError error={new Error("backend unavailable")} reset={reset} />);

    expect(screen.getByRole("heading", { name: "Не удалось загрузить товар" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
