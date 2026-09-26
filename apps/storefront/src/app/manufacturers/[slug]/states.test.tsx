import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import ManufacturerError from "./error";
import ManufacturerNotFound from "./not-found";

describe("manufacturer route states", () => {
  it("renders a useful not-found state", () => {
    render(<ManufacturerNotFound />);

    expect(screen.getByRole("heading", { name: "Производитель не найден" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "К списку производителей" })).toHaveAttribute("href", "/manufacturers");
  });

  it("renders backend failure as a recoverable error", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<ManufacturerError error={new Error("backend unavailable")} reset={reset} />);

    expect(screen.getByRole("heading", { name: "Не удалось загрузить производителя" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
