import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import CollectionError from "./error";
import CollectionNotFound from "./not-found";

describe("collection route states", () => {
  it("renders a useful not-found state", () => {
    render(<CollectionNotFound />);

    expect(screen.getByRole("heading", { name: "Подборка не найдена" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "К списку подборок" })).toHaveAttribute("href", "/collections");
  });

  it("renders backend or malformed-response failure as a recoverable error", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<CollectionError error={new Error("backend unavailable")} reset={reset} />);

    expect(screen.getByRole("heading", { name: "Не удалось загрузить подборку" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
