import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import CategoriesError from "./error";
import CategoriesLoading from "./loading";

describe("category directory route states", () => {
  it("renders a busy loading state", () => {
    render(<CategoriesLoading />);

    expect(screen.getByRole("main")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("heading", { name: "Загружаем категории" })).toBeInTheDocument();
  });

  it("renders backend failure as a recoverable error", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<CategoriesError error={Object.assign(new Error("down"), { digest: "d-1" })} reset={reset} />);

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Не удалось загрузить категории" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Открыть каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByText("Код обращения: d-1")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
