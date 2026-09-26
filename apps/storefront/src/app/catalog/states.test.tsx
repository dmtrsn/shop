import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import CatalogError from "./error";

describe("catalog route states", () => {
  it("renders a recoverable backend failure rather than an empty catalog", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<CatalogError error={new Error("backend unavailable")} reset={reset} />);

    expect(screen.getByRole("heading", { name: "Не удалось загрузить товары" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Ничего не найдено" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
