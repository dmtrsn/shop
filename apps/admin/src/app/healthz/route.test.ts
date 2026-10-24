// @vitest-environment node
import { describe, expect, it } from "vitest";

import { GET } from "./route";

describe("admin health route", () => {
  it("returns a small non-cacheable readiness response", async () => {
    const response = GET();
    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.json()).resolves.toEqual({ status: "ok" });
  });
});
