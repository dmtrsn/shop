import { describe, expect, it } from "vitest";

import type { Category } from "./api/catalog";
import {
  ancestorsOf,
  buildCategoryTree,
  categoryCatalogUrl,
  categoryUrl,
  findCategoryNode,
  flattenCategoryTree,
  rootOf,
} from "./category-tree";

function category(id: string, parentId?: string, slug = id): Category {
  return { id, slug, name: id.toUpperCase(), parentId, productCount: 0 };
}

function flatIds(categories: Category[]): string[] {
  return flattenCategoryTree(buildCategoryTree(categories)).map((node) => node.category.id);
}

describe("category tree", () => {
  it("preserves API order among roots and siblings", () => {
    const tree = buildCategoryTree([
      category("candy"),
      category("truffles", "candy"),
      category("chocolate"),
      category("praline", "candy"),
    ]);

    expect(tree.roots.map((node) => node.category.id)).toEqual(["candy", "chocolate"]);
    expect(tree.roots[0].children.map((node) => node.category.id)).toEqual(["truffles", "praline"]);
  });

  it("nests three or more levels and reports depth and ancestry", () => {
    const tree = buildCategoryTree([
      category("dark", "bars"),
      category("chocolate"),
      category("bars", "chocolate"),
      category("single-origin", "dark"),
    ]);
    const leaf = findCategoryNode(tree, "single-origin");

    expect(leaf?.depth).toBe(3);
    expect(ancestorsOf(leaf!).map((node) => node.category.id)).toEqual(["chocolate", "bars", "dark"]);
    expect(rootOf(leaf!).category.id).toBe("chocolate");
    expect(flattenCategoryTree(tree).map((node) => [node.category.id, node.depth])).toEqual([
      ["chocolate", 0],
      ["bars", 1],
      ["dark", 2],
      ["single-origin", 3],
    ]);
  });

  it("treats a category with an unknown parent as a root", () => {
    const tree = buildCategoryTree([category("orphan", "deleted-parent")]);

    expect(tree.roots.map((node) => node.category.id)).toEqual(["orphan"]);
    expect(tree.roots[0].depth).toBe(0);
  });

  it("places self-parented and cyclic categories exactly once", () => {
    const categories = [
      category("self", "self"),
      category("a", "b"),
      category("b", "a"),
      category("c", "b"),
    ];
    const tree = buildCategoryTree(categories);

    expect(flatIds(categories).sort()).toEqual(["a", "b", "c", "self"]);
    expect(tree.roots.map((node) => node.category.id)).toEqual(["self", "a"]);
    expect(findCategoryNode(tree, "self")?.children).toEqual([]);
    expect(ancestorsOf(findCategoryNode(tree, "c")!).map((node) => node.category.id)).toEqual(["a", "b"]);
  });

  it("keeps the first occurrence of a duplicate ID", () => {
    const tree = buildCategoryTree([
      { ...category("dup"), name: "First" },
      { ...category("dup"), name: "Second" },
    ]);

    expect(tree.roots).toHaveLength(1);
    expect(tree.roots[0].category.name).toBe("First");
  });

  it("resolves a slug before an ID and supports ID lookup", () => {
    const tree = buildCategoryTree([
      category("cat-a", undefined, "shared"),
      category("shared", undefined, "other"),
    ]);

    expect(findCategoryNode(tree, "shared")?.category.id).toBe("cat-a");
    expect(findCategoryNode(tree, "cat-a")?.category.slug).toBe("shared");
    expect(findCategoryNode(tree, "missing")).toBeUndefined();
    expect(findCategoryNode(tree, undefined)).toBeUndefined();
  });

  it("builds canonical category and catalog URLs", () => {
    expect(categoryUrl("gift-boxes")).toBe("/categories/gift-boxes");
    expect(categoryUrl("сладости и/или")).toBe("/categories/%D1%81%D0%BB%D0%B0%D0%B4%D0%BE%D1%81%D1%82%D0%B8%20%D0%B8%2F%D0%B8%D0%BB%D0%B8");
    expect(categoryCatalogUrl("chocolate")).toBe("/catalog?category=chocolate");
  });
});
