import {
  parseBrand,
  parseManufacturer,
  type Brand,
  type Manufacturer,
} from "./catalog";
import {
  ApiRequestError,
  type ApiRequestOptions,
  isRecord,
  requestApiJson,
} from "./request";

export type ProducerDirectory = {
  manufacturers: Manufacturer[];
  brands: Brand[];
};

export type ManufacturerPageData = {
  manufacturer: Manufacturer;
  brands: Brand[];
};

export class ProducerApiError extends Error {
  readonly kind: ApiRequestError["kind"];
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ApiRequestError["kind"],
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "ProducerApiError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

function envelopeItems(value: unknown, key: string): unknown[] {
  if (!isRecord(value) || !Array.isArray(value.items)) {
    throw new Error(`${key}.items must be an array`);
  }
  return value.items;
}

function producerError(error: unknown): ProducerApiError {
  if (error instanceof ProducerApiError) return error;
  if (error instanceof ApiRequestError) {
    return new ProducerApiError(error.kind, error.message, {
      cause: error,
      requestId: error.requestId,
      status: error.status,
    });
  }
  return new ProducerApiError(
    "invalid-response",
    "The producer response did not match its contract",
    { cause: error },
  );
}

export async function getProducerDirectory(
  options: ApiRequestOptions = {},
): Promise<ProducerDirectory> {
  try {
    const [manufacturers, brands] = await Promise.all([
      getManufacturers(options),
      getBrands(options),
    ]);
    return { manufacturers, brands };
  } catch (error) {
    throw producerError(error);
  }
}

export async function getManufacturers(
  options: ApiRequestOptions = {},
): Promise<Manufacturer[]> {
  try {
    const response = await requestApiJson("/api/manufacturers", options);
    return envelopeItems(response, "manufacturers").map(parseManufacturer);
  } catch (error) {
    throw producerError(error);
  }
}

export async function getBrands(
  options: ApiRequestOptions = {},
): Promise<Brand[]> {
  try {
    const response = await requestApiJson("/api/brands", options);
    return envelopeItems(response, "brands").map(parseBrand);
  } catch (error) {
    throw producerError(error);
  }
}

function matchesIdentity(
  manufacturer: Pick<Manufacturer, "id" | "slug">,
  identity: string,
): boolean {
  return manufacturer.id === identity || manufacturer.slug === identity;
}

export async function getManufacturerPageData(
  identity: string,
  options: ApiRequestOptions = {},
): Promise<ManufacturerPageData | undefined> {
  try {
    const directory = await getProducerDirectory(options);
    const manufacturer = directory.manufacturers.find((item) =>
      matchesIdentity(item, identity),
    );
    if (!manufacturer) return undefined;

    const brands = directory.brands.filter(
      (brand) => brand.manufacturerId === manufacturer.id,
    );
    return { manufacturer, brands };
  } catch (error) {
    throw producerError(error);
  }
}
