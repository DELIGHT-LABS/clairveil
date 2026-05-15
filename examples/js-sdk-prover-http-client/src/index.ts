import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

interface Proof {
  version: string;
  payload_hash: string;
  proof_hex: string;
}

interface TransferProofRequest {
  version: string;
  payload: {
    payload_hash: string;
  };
}

interface TransferProofResponse {
  version: string;
  proof: Proof;
}

interface WithdrawProofRequest {
  version: string;
  payload: {
    payload_hash: string;
  };
}

interface WithdrawProofResponse {
  version: string;
  proof: Proof;
}

interface ProverExampleBundle {
  transfer: {
    request: TransferProofRequest;
    response: TransferProofResponse;
  };
  withdraw: {
    request: WithdrawProofRequest;
    response: WithdrawProofResponse;
  };
}

interface ProverClientOptions {
  baseURL: string;
  bearerToken?: string;
  timeoutMs: number;
}

interface ProverClient {
  proveTransfer(request: TransferProofRequest): Promise<TransferProofResponse>;
  proveWithdraw(request: WithdrawProofRequest): Promise<WithdrawProofResponse>;
}

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../..");
const fixturePath = join(repoRoot, "x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json");
const transferPath = "/v1/prover/transfer";
const withdrawPath = "/v1/prover/withdraw";

function readFixture(): ProverExampleBundle {
  return JSON.parse(readFileSync(fixturePath, "utf8")) as ProverExampleBundle;
}

function createProverClient(options: ProverClientOptions): ProverClient {
  if (options.timeoutMs <= 0) {
    throw new Error("timeoutMs must be positive");
  }

  return {
    proveTransfer(request) {
      return postJSON<TransferProofRequest, TransferProofResponse>(options, transferPath, request);
    },
    proveWithdraw(request) {
      return postJSON<WithdrawProofRequest, WithdrawProofResponse>(options, withdrawPath, request);
    },
  };
}

async function postJSON<RequestBody, ResponseBody>(
  options: ProverClientOptions,
  path: string,
  body: RequestBody,
): Promise<ResponseBody> {
  const baseURL = new URL(options.baseURL);
  if (baseURL.protocol !== "http:" && baseURL.protocol !== "https:") {
    throw new Error(`unsupported prover URL protocol ${baseURL.protocol}`);
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), options.timeoutMs);
  try {
    const response = await fetch(new URL(path, baseURL), {
      method: "POST",
      headers: requestHeaders(options.bearerToken),
      body: JSON.stringify(body),
      signal: controller.signal,
    });

    const responseText = await response.text();
    if (!response.ok) {
      throw new Error(`prover request failed with status ${response.status}: ${responseText}`);
    }
    return JSON.parse(responseText) as ResponseBody;
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      throw new Error(`prover request timed out after ${options.timeoutMs}ms`);
    }
    throw err;
  } finally {
    clearTimeout(timeout);
  }
}

function requestHeaders(bearerToken?: string): Headers {
  const headers = new Headers();
  headers.set("Content-Type", "application/json");
  if (bearerToken && bearerToken.trim() !== "") {
    headers.set("Authorization", `Bearer ${bearerToken}`);
  }
  return headers;
}

function validateTransferResponse(request: TransferProofRequest, response: TransferProofResponse): void {
  assertEqual(request.version, "v1", "transfer request version");
  assertEqual(response.version, "v1", "transfer response version");
  assertEqual(response.proof.version, "v1", "transfer proof version");
  assertEqual(response.proof.payload_hash, request.payload.payload_hash, "transfer proof payload_hash");
}

function validateWithdrawResponse(request: WithdrawProofRequest, response: WithdrawProofResponse): void {
  assertEqual(request.version, "v1", "withdraw request version");
  assertEqual(response.version, "v1", "withdraw response version");
  assertEqual(response.proof.version, "v1", "withdraw proof version");
  assertEqual(response.proof.payload_hash, request.payload.payload_hash, "withdraw proof payload_hash");
}

function assertEqual(actual: unknown, expected: unknown, label: string): void {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

async function startMockProver(bundle: ProverExampleBundle, bearerToken: string): Promise<{ baseURL: string; close(): Promise<void> }> {
  const server = createServer(async (request, response) => {
    try {
      if (!authorized(request, bearerToken)) {
        writeJSON(response, 401, { version: "v1", code: "unauthorized", message: "missing or invalid bearer token" });
        return;
      }

      if (request.method !== "POST") {
        writeJSON(response, 405, { version: "v1", code: "method_not_allowed", message: "proof route requires POST" });
        return;
      }

      if (request.url === transferPath) {
        const body = await readJSONBody<TransferProofRequest>(request);
        assertEqual(body.payload.payload_hash, bundle.transfer.request.payload.payload_hash, "mock transfer payload_hash");
        writeJSON(response, 200, bundle.transfer.response);
        return;
      }

      if (request.url === withdrawPath) {
        const body = await readJSONBody<WithdrawProofRequest>(request);
        assertEqual(body.payload.payload_hash, bundle.withdraw.request.payload.payload_hash, "mock withdraw payload_hash");
        writeJSON(response, 200, bundle.withdraw.response);
        return;
      }

      writeJSON(response, 404, { version: "v1", code: "not_found", message: "route not found" });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      writeJSON(response, 400, { version: "v1", code: "invalid_request", message });
    }
  });

  await new Promise<void>((resolveListen) => {
    server.listen(0, "127.0.0.1", resolveListen);
  });
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("failed to bind mock prover");
  }

  return {
    baseURL: `http://127.0.0.1:${address.port}`,
    close: () => new Promise<void>((resolveClose, rejectClose) => {
      server.close((err) => {
        if (err) {
          rejectClose(err);
          return;
        }
        resolveClose();
      });
    }),
  };
}

function authorized(request: IncomingMessage, bearerToken: string): boolean {
  return request.headers.authorization === `Bearer ${bearerToken}`;
}

async function readJSONBody<T>(request: IncomingMessage): Promise<T> {
  const chunks: Uint8Array[] = [];
  for await (const chunk of request) {
    chunks.push(typeof chunk === "string" ? Buffer.from(chunk) : chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as T;
}

function writeJSON(response: ServerResponse, statusCode: number, body: unknown): void {
  response.writeHead(statusCode, { "Content-Type": "application/json" });
  response.end(JSON.stringify(body));
}

async function main(): Promise<void> {
  const bundle = readFixture();
  const bearerToken = randomBytes(16).toString("hex");
  const mockProver = await startMockProver(bundle, bearerToken);

  try {
    const client = createProverClient({
      baseURL: mockProver.baseURL,
      bearerToken,
      timeoutMs: 5_000,
    });

    const transferResponse = await client.proveTransfer(bundle.transfer.request);
    validateTransferResponse(bundle.transfer.request, transferResponse);

    const withdrawResponse = await client.proveWithdraw(bundle.withdraw.request);
    validateWithdrawResponse(bundle.withdraw.request, withdrawResponse);

    console.log("Clairveil JS prover HTTP client demo passed");
    console.log(`- prover base URL: ${mockProver.baseURL}`);
    console.log(`- transfer proof payload hash: ${transferResponse.proof.payload_hash}`);
    console.log(`- withdraw proof payload hash: ${withdrawResponse.proof.payload_hash}`);
  } finally {
    await mockProver.close();
  }
}

await main();
