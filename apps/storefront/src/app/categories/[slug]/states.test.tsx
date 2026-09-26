import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import CategoryError from "./error";
import CategoryNotFound from "./not-found";

describe("category route states", () => {
  it("renders a useful not-found state", () => {
    render(<CategoryNotFound />);

    expect(screen.getByRole("heading", { name: "Категория не найдена" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "К списку категорий" })).toHaveAttribute("href", "/categories");
  });

  it("renders backend failure as a recoverable error", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<CategoryError error={new Error("backend unavailable")} reset={reset} />);

    expect(screen.getByRole("heading", { name: "Не удалось загрузить категорию" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
