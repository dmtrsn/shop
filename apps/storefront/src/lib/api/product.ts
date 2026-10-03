import {
  ApiRequestError,
  type ApiRequestOptions,
  requestApiJsonWithMeta,
} from "./request";
import { parseProduct, type Product } from "./product-dto";

export { parseProduct, type Product, type ProductEntity } from "./product-dto";

export type ProductApiErrorKind =
  | "cancelled"
  | "configuration"
  | "invalid-response"
  | "not-found"
  | "unavailable";

export class ProductApiError extends Error {
  readonly kind: ProductApiErrorKind;
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ProductApiErrorKind,
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "ProductApiError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

export async function getProduct(
  identity: string,
  options: ApiRequestOptions = {},
): Promise<Product> {
  let response: Awaited<ReturnType<typeof requestApiJsonWithMeta>>;
  try {
    response = await requestApiJsonWithMeta(
      `/api/products/${encodeURIComponent(identity)}`,
      options,
    );
  } catch (error) {
    if (error instanceof ApiRequestError) {
      if (error.status === 404) {
        throw new ProductApiError("not-found", "Product not found", {
          cause: error,
          requestId: error.requestId,
          status: error.status,
        });
      }
      throw new ProductApiError(error.kind, error.message, {
        cause: error,
        requestId: error.requestId,
        status: error.status,
      });
    }
    throw error;
  }

  try {
    return parseProduct(response.body);
  } catch (error) {
    throw new ProductApiError("invalid-response", "The product response did not match its contract", {
      cause: error,
      requestId: response.requestId,
      status: response.status,
    });
  }
}
