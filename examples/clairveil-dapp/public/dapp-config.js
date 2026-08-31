// Static deployments load this artifact directly. Keep the release gate bound
// to the same same-origin path instead of injecting an unverified runtime
// override into the browser bundle.
export const staticDappConfigPath = "/dapp-config.json";
export const staticDappConfigRequestTimeoutMs = 30_000;
export const staticDappConfigResponseMaxBytes = 1 << 20;

function responseContentLength(response) {
  const raw = response?.headers?.get?.("content-length");
  if (!raw || !/^(0|[1-9][0-9]*)$/.test(raw.trim())) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) ? value : null;
}

async function readBoundedStaticConfigText(response, maxResponseBytes) {
  const declaredLength = responseContentLength(response);
  if (declaredLength !== null && declaredLength > maxResponseBytes) {
    throw new Error(
      `Static Clairveil WebApp configuration exceeds ${maxResponseBytes} byte limit`,
    );
  }
  if (!response?.body || typeof response.body.getReader !== "function") {
    const text = await response.text();
    if (new TextEncoder().encode(text).byteLength > maxResponseBytes) {
      throw new Error(
        `Static Clairveil WebApp configuration exceeds ${maxResponseBytes} byte limit`,
      );
    }
    return text;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let totalBytes = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    if (!value) continue;
    totalBytes += value.byteLength;
    if (totalBytes > maxResponseBytes) {
      await reader.cancel().catch(() => {});
      throw new Error(
        `Static Clairveil WebApp configuration exceeds ${maxResponseBytes} byte limit`,
      );
    }
    chunks.push(value);
  }
  const bytes = new Uint8Array(totalBytes);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(bytes);
}

function staticConfigTimeoutError(timeoutMs, cause) {
  const error = new Error(
    `Static Clairveil WebApp configuration timed out after ${timeoutMs}ms`,
  );
  error.code = "request_timeout";
  error.cause = cause;
  return error;
}

function expectedStaticConfigUrl(locationHref) {
  if (!locationHref) return null;
  try {
    return new URL(staticDappConfigPath, locationHref);
  } catch {
    return null;
  }
}

function assertDirectStaticConfigResponse(response, locationHref) {
  if (response?.redirected === true) {
    throw new Error("Static Clairveil WebApp configuration must not redirect");
  }
  const expected = expectedStaticConfigUrl(locationHref);
  const finalUrl = String(response?.url || "");
  if (!expected || !finalUrl) return;
  let actual;
  try {
    actual = new URL(finalUrl);
  } catch {
    throw new Error("Static Clairveil WebApp configuration response URL is invalid");
  }
  if (actual.href !== expected.href) {
    throw new Error(
      "Static Clairveil WebApp configuration must be served directly from the same-origin /dapp-config.json artifact",
    );
  }
}

function assertJsonConfigResponse(response) {
  const contentType = String(response?.headers?.get?.("content-type") || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
  if (contentType !== "application/json") {
    throw new Error(
      "Static Clairveil WebApp configuration must return Content-Type: application/json",
    );
  }
}

export async function loadStaticDappConfig({
  fetchImpl = globalThis.fetch,
  locationHref = globalThis.location?.href || "",
  timeoutMs = staticDappConfigRequestTimeoutMs,
  maxResponseBytes = staticDappConfigResponseMaxBytes,
} = {}) {
  if (typeof fetchImpl !== "function") {
    throw new Error("Static Clairveil WebApp configuration fetch is unavailable");
  }
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1) {
    throw new Error("Static Clairveil WebApp configuration timeout must be a positive integer");
  }
  if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) {
    throw new Error("Static Clairveil WebApp configuration response limit must be a positive integer");
  }
  if (typeof AbortController !== "function") {
    throw new Error("AbortController is required for static Clairveil WebApp configuration");
  }

  const controller = new AbortController();
  let timedOut = false;
  const timeoutID = globalThis.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);

  let response;
  let body;
  try {
    response = await fetchImpl(staticDappConfigPath, {
      cache: "no-store",
      redirect: "error",
      headers: { accept: "application/json" },
      signal: controller.signal,
    });
    if (!response?.ok) {
      throw new Error(
        `Static Clairveil WebApp configuration returned HTTP ${response?.status || "unknown"}`,
      );
    }
    assertDirectStaticConfigResponse(response, locationHref);
    assertJsonConfigResponse(response);
    body = await readBoundedStaticConfigText(response, maxResponseBytes);
  } catch (cause) {
    if (timedOut) {
      throw staticConfigTimeoutError(timeoutMs, cause);
    }
    throw new Error(
      `Static Clairveil WebApp configuration is unavailable: ${cause?.message || String(cause)}`,
      { cause },
    );
  } finally {
    globalThis.clearTimeout(timeoutID);
  }

  let config;
  try {
    config = JSON.parse(body);
  } catch {
    throw new Error("Static Clairveil WebApp configuration must return JSON");
  }
  if (!config || typeof config !== "object" || Array.isArray(config)) {
    throw new Error("Static Clairveil WebApp configuration is invalid");
  }
  if (config.serverBacked !== false) {
    throw new Error("Static Clairveil WebApp configuration must set serverBacked to false");
  }
  return config;
}
