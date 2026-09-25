import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { QuantityControl } from "./quantity-control";

function ControlledQuantity() {
  const [value, setValue] = useState(2);
  return <QuantityControl label="Количество коробок" min={2} onChange={setValue} step={2} value={value} />;
}

describe("QuantityControl", () => {
  it("applies the configured step and respects the minimum", async () => {
    const user = userEvent.setup();
    render(<ControlledQuantity />);

    const input = screen.getByRole("spinbutton", { name: "Количество коробок" });
    const decrement = screen.getByRole("button", { name: "Уменьшить: количество коробок" });
    const increment = screen.getByRole("button", { name: "Увеличить: количество коробок" });

    expect(input).toHaveValue(2);
    expect(decrement).toBeDisabled();

    await user.click(increment);
    expect(input).toHaveValue(4);
    expect(decrement).toBeEnabled();

    await user.click(decrement);
    expect(input).toHaveValue(2);
    expect(decrement).toBeDisabled();
  });

  it("does not emit changes while disabled", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<QuantityControl disabled onChange={onChange} value={3} />);

    await user.click(screen.getByRole("button", { name: /увеличить/i }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("spinbutton")).toBeDisabled();
  });
});
