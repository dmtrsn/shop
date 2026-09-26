import {
  ancestorsOf,
  buildCategoryTree,
  findCategoryNode,
} from "../category-tree";
import { parseCategory, type Category } from "./catalog";
import {
  ApiRequestError,
  type ApiRequestOptions,
  isRecord,
  requestApiJsonWithMeta,
} from "./request";

export type CategoryPageData = {
  category: Category;
  ancestors: Category[];
  parent?: Category;
  children: Category[];
  /** Other categories at the same level, including the current one. */
  siblings: Category[];
};

export class CategoryApiError extends Error {
  readonly kind: ApiRequestError["kind"];
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ApiRequestError["kind"],
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "CategoryApiError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

export async function getCategories(
  options: ApiRequestOptions = {},
): Promise<Category[]> {
  let response: Awaited<ReturnType<typeof requestApiJsonWithMeta>>;
  try {
    response = await requestApiJsonWithMeta("/api/categories", options);
  } catch (error) {
    if (error instanceof ApiRequestError) {
      throw new CategoryApiError(error.kind, error.message, {
        cause: error,
        requestId: error.requestId,
        status: error.status,
      });
    }
    throw error;
  }

  try {
    if (!isRecord(response.body) || !Array.isArray(response.body.items)) {
      throw new Error("categories must contain an items array");
    }
    return response.body.items.map(parseCategory);
  } catch (error) {
    throw new CategoryApiError(
      "invalid-response",
      "The category response did not match its contract",
      { cause: error, requestId: response.requestId, status: response.status },
    );
  }
}

export async function getCategoryPageData(
  identity: string,
  options: ApiRequestOptions = {},
): Promise<CategoryPageData | undefined> {
  const tree = buildCategoryTree(await getCategories(options));
  const node = findCategoryNode(tree, identity);
  if (!node) return undefined;

  return {
    category: node.category,
    ancestors: ancestorsOf(node).map((item) => item.category),
    parent: node.parent?.category,
    children: node.children.map((item) => item.category),
    siblings: (node.parent?.children ?? tree.roots).map((item) => item.category),
  };
}
