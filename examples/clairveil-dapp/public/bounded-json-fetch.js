export const defaultBoundedJsonTimeoutMs = 30_000;
export const defaultBoundedJsonMaxBytes = 1024 * 1024;

function positiveSafeInteger(value, label) {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new TypeError(`${label} must be a positive safe integer`);
  }
  return parsed;
}

function abortReason(signal) {
  if (signal?.reason instanceof Error) return signal.reason;
  return new DOMException("The request was aborted", "AbortError");
}

function abortable(promise, signal) {
  if (signal.aborted) return Promise.reject(abortReason(signal));
  return new Promise((resolve, reject) => {
    const settle = callback => value => {
      signal.removeEventListener("abort", onAbort);
      callback(value);
    };
    const onAbort = () => settle(reject)(abortReason(signal));
    signal.addEventListener("abort", onAbort, { once: true });
    Promise.resolve(promise).then(settle(resolve), settle(reject));
  });
}

function contentType(response) {
  return String(response.headers.get("content-type") || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
}

function assertDirectResponse(response, expectedUrl, label) {
  if (response.type === "opaqueredirect"
    || response.redirected
    || (response.status >= 300 && response.status < 400)) {
    throw new Error(`${label} must not redirect`);
  }
  if (String(response.url || "").trim() !== expectedUrl) {
    throw new Error(`${label} final URL does not match the configured endpoint`);
  }
}

async function readBoundedResponseBytes(response, maxBytes, signal, label) {
  const declaredLength = String(response.headers.get("content-length") || "").trim();
  if (/^\d+$/.test(declaredLength) && Number(declaredLength) > maxBytes) {
    throw new Error(`${label} response exceeds ${maxBytes} bytes`);
  }

  if (!response.body?.getReader) {
    const buffer = await abortable(response.arrayBuffer(), signal);
    const bytes = new Uint8Array(buffer);
    if (bytes.byteLength > maxBytes) {
      throw new Error(`${label} response exceeds ${maxBytes} bytes`);
    }
    return bytes;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let length = 0;
  try {
    while (true) {
      const { done, value } = await abortable(reader.read(), signal);
      if (done) break;
      const chunk = value instanceof Uint8Array ? value : new Uint8Array(value || []);
      length += chunk.byteLength;
      if (length > maxBytes) {
        throw new Error(`${label} response exceeds ${maxBytes} bytes`);
      }
      chunks.push(chunk);
    }
  } catch (error) {
    void reader.cancel(error).catch(() => {});
    throw error;
  } finally {
    try {
      reader.releaseLock();
    } catch {
      // A pending read is cancelled above; releasing is best effort only.
    }
  }

  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes;
}

export async function fetchBoundedJson(url, {
  fetchImpl = globalThis.fetch,
  signal,
  timeoutMs = defaultBoundedJsonTimeoutMs,
  maxBytes = defaultBoundedJsonMaxBytes,
  label = "JSON request",
  cache = "no-store"
} = {}) {
  if (typeof fetchImpl !== "function") {
    throw new Error(`${label} requires fetch support`);
  }
  const resolvedTimeoutMs = positiveSafeInteger(timeoutMs, "timeoutMs");
  const resolvedMaxBytes = positiveSafeInteger(maxBytes, "maxBytes");
  const expectedUrl = new URL(String(url), globalThis.location?.href).href;
  const controller = new AbortController();
  const timeoutError = new Error(`${label} timed out after ${resolvedTimeoutMs}ms`);
  timeoutError.name = "TimeoutError";
  timeoutError.code = "BOUNDED_JSON_TIMEOUT";
  const onCallerAbort = () => controller.abort(abortReason(signal));
  if (signal?.aborted) {
    onCallerAbort();
  } else {
    signal?.addEventListener("abort", onCallerAbort, { once: true });
  }
  const timeout = globalThis.setTimeout(() => controller.abort(timeoutError), resolvedTimeoutMs);

  try {
    const response = await abortable(fetchImpl(expectedUrl, {
      cache,
      headers: { Accept: "application/json" },
      redirect: "error",
      signal: controller.signal
    }), controller.signal);
    assertDirectResponse(response, expectedUrl, label);
    if (!response.ok) {
      throw new Error(`${label} failed with HTTP ${response.status}`);
    }
    const responseType = contentType(response);
    if (responseType !== "application/json" && !responseType.endsWith("+json")) {
      throw new Error(`${label} must return JSON`);
    }
    const bytes = await readBoundedResponseBytes(
      response,
      resolvedMaxBytes,
      controller.signal,
      label
    );
    try {
      return JSON.parse(new TextDecoder().decode(bytes));
    } catch (error) {
      throw new Error(`${label} returned invalid JSON`, { cause: error });
    }
  } finally {
    globalThis.clearTimeout(timeout);
    signal?.removeEventListener("abort", onCallerAbort);
  }
}
