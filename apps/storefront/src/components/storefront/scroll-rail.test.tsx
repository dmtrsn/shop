import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ScrollRail } from "./scroll-rail";

describe("ScrollRail", () => {
  it("is a plain list whose focused item is brought fully into view", () => {
    render(<ScrollRail><li><a href="/manufacturers/a">A</a></li><li><a href="/manufacturers/b">B</a></li></ScrollRail>);
    const link = screen.getByRole("link", { name: "B" });
    const scrollIntoView = vi.fn();
    link.scrollIntoView = scrollIntoView;
    fireEvent.focus(link);
    expect(scrollIntoView).toHaveBeenCalledExactlyOnceWith({ block: "nearest", inline: "nearest" });
    expect(screen.getByRole("list")).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });
});
