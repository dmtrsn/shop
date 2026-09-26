import type { Category } from "./api/catalog";
import { catalogUrl, parseCatalogState } from "./catalog-state";

export type CategoryNode = {
  category: Category;
  parent?: CategoryNode;
  children: CategoryNode[];
  depth: number;
};

export type CategoryTree = {
  roots: CategoryNode[];
  byId: Map<string, CategoryNode>;
  bySlug: Map<string, CategoryNode>;
};

// The API returns a flat adjacency list and the schema does not prevent
// self-parents, cycles, or dangling parents. Every category is placed exactly
// once: unknown parents become roots, and a cycle is broken at its first
// member in API order. API order is preserved among siblings.
export function buildCategoryTree(categories: Category[]): CategoryTree {
  const unique: Category[] = [];
  const categoryById = new Map<string, Category>();
  for (const category of categories) {
    if (categoryById.has(category.id)) continue;
    categoryById.set(category.id, category);
    unique.push(category);
  }

  const childrenByParent = new Map<string, Category[]>();
  for (const category of unique) {
    if (!category.parentId || !categoryById.has(category.parentId)) continue;
    childrenByParent.set(category.parentId, [
      ...(childrenByParent.get(category.parentId) ?? []),
      category,
    ]);
  }

  const tree: CategoryTree = { roots: [], byId: new Map(), bySlug: new Map() };

  function register(category: Category, parent?: CategoryNode): CategoryNode {
    const node: CategoryNode = {
      category,
      parent,
      children: [],
      depth: parent ? parent.depth + 1 : 0,
    };
    parent?.children.push(node);
    tree.byId.set(category.id, node);
    if (!tree.bySlug.has(category.slug)) tree.bySlug.set(category.slug, node);
    return node;
  }

  // Iterative, and each ID is registered once, so a cycle cannot recurse forever.
  function placeRoot(category: Category) {
    const pending = [register(category)];
    tree.roots.push(pending[0]);
    while (pending.length) {
      const current = pending.pop() as CategoryNode;
      for (const child of childrenByParent.get(current.category.id) ?? []) {
        if (!tree.byId.has(child.id)) pending.push(register(child, current));
      }
    }
  }

  for (const category of unique) {
    if (!category.parentId || !categoryById.has(category.parentId)) placeRoot(category);
  }
  for (const category of unique) {
    if (!tree.byId.has(category.id)) placeRoot(category);
  }
  return tree;
}

export function findCategoryNode(
  tree: CategoryTree,
  identity: string | undefined,
): CategoryNode | undefined {
  if (!identity) return undefined;
  return tree.bySlug.get(identity) ?? tree.byId.get(identity);
}

/** Ancestors from the root down to the direct parent. */
export function ancestorsOf(node: CategoryNode): CategoryNode[] {
  const ancestors: CategoryNode[] = [];
  for (let current = node.parent; current; current = current.parent) {
    ancestors.unshift(current);
  }
  return ancestors;
}

export function rootOf(node: CategoryNode): CategoryNode {
  return ancestorsOf(node)[0] ?? node;
}

/** Pre-order traversal, suitable for an indented option list. */
export function flattenCategoryTree(tree: CategoryTree): CategoryNode[] {
  const result: CategoryNode[] = [];
  const visit = (node: CategoryNode) => {
    result.push(node);
    node.children.forEach(visit);
  };
  tree.roots.forEach(visit);
  return result;
}

export function categoryUrl(slug: string): string {
  return `/categories/${encodeURIComponent(slug)}`;
}

export function categoryCatalogUrl(slug: string): string {
  return catalogUrl(parseCatalogState({}), { category: slug });
}
