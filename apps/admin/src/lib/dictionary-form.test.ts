import { describe, expect, it } from "vitest";

import { collection } from "@/test/api-mock";
import { dictionaryErrorsFromServer, emptyDictionaryForm, formFromEntry, moscowDay, payloadFromDictionaryForm } from "./dictionary-form";

const form = (overrides = {}) => ({ ...emptyDictionaryForm, name: " Джемы ", slug: "jams", sortOrder: "5", ...overrides });

describe("dictionary form", () => {
  it("builds each dictionary's exact write document", () => {
    expect(payloadFromDictionaryForm("categories", form({ parentId: "cat-choc", icon: "jar" }))).toEqual({
      ok: true,
      payload: { slug: "jams", name: "Джемы", description: "", sortOrder: 5, parentId: "cat-choc", icon: "jar" },
    });
    expect(payloadFromDictionaryForm("manufacturers", form({ country: " Россия " }))).toEqual({
      ok: true,
      payload: { slug: "jams", name: "Джемы", description: "", sortOrder: 5, country: "Россия" },
    });
    expect(payloadFromDictionaryForm("brands", form({ manufacturerId: "m-choco" }))).toEqual({
      ok: true,
      payload: { slug: "jams", name: "Джемы", description: "", sortOrder: 5, manufacturerId: "m-choco" },
    });
  });

  it("turns collection days into whole Moscow days and open ends into null", () => {
    expect(payloadFromDictionaryForm("collections", form({ accent: "sky", startsAt: "2026-12-01", endsAt: "" }))).toEqual({
      ok: true,
      payload: { slug: "jams", name: "Джемы", description: "", sortOrder: 5, accent: "sky", startsAt: "2026-12-01T00:00:00+03:00", endsAt: null },
    });
    const ends = payloadFromDictionaryForm("collections", form({ endsAt: "2027-01-10" }));
    expect(ends.ok && ends.payload.endsAt).toBe("2027-01-10T23:59:59+03:00");
    // Round trip through Go's UTC timestamps keeps the same calendar days.
    expect(moscowDay("2026-11-30T21:00:00Z")).toBe("2026-12-01");
    expect(moscowDay("2027-01-10T20:59:59Z")).toBe("2027-01-10");
    expect(formFromEntry("collections", collection({ startsAt: "2026-11-30T21:00:00Z" }))).toMatchObject({ startsAt: "2026-12-01", endsAt: "", accent: "coral" });
  });

  it("reports every typo before calling Go", () => {
    const result = payloadFromDictionaryForm("collections", form({ name: "", slug: "Не slug", sortOrder: "-1", accent: "neon", startsAt: "2027-02-01", endsAt: "2027-01-01" }));
    expect(result.ok).toBe(false);
    expect(!result.ok && Object.keys(result.errors).sort()).toEqual(["accent", "endsAt", "name", "slug", "sortOrder"]);
    const brand = payloadFromDictionaryForm("brands", form());
    expect(!brand.ok && brand.errors).toEqual({ manufacturerId: "Выберите производителя" });
    const category = payloadFromDictionaryForm("categories", form({ icon: "<svg>" }));
    expect(!category.ok && Object.keys(category.errors)).toEqual(["icon"]);
  });

  it("puts Go's field errors next to the dictionary's own fields only", () => {
    expect(dictionaryErrorsFromServer("brands", [{ field: "slug", message: "занят" }, { field: "parentId", message: "чужое" }, { field: "body", message: "плохо" }])).toEqual({
      errors: { slug: "занят" },
      other: ["чужое", "плохо"],
    });
  });
});
