import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { randomBytes } from "node:crypto";

const route = "/v2/prover/audit-field";
const circuitSetID = "privacy-note-v1-audit-field-v1";
const circuitID = "deposit-audit-field-v1";
const pi23Length = 23;

interface AuditRequest {
  version: "v1"; circuit_set_id: string; circuit_id: string;
  artifact_hash: string; public_inputs: string[]; witness: string;
}
interface AuditResponse {
  version: "v1"; circuit_set_id: string; circuit_id: string;
  artifact_hash: string; public_inputs: string[]; proof: string;
}

function equal(actual: unknown, expected: unknown, label: string): void {
  if (actual !== expected) throw new Error(label + ": expected " + String(expected) + ", got " + String(actual));
}
function bytes(value: string, label: string, length?: number): Buffer {
  if (!value) throw new Error(label + " must be non-empty base64");
  const decoded = Buffer.from(value, "base64");
  if (!decoded.length || decoded.toString("base64") !== value) throw new Error(label + " must be canonical base64");
  if (length !== undefined && decoded.length !== length) throw new Error(label + " must have " + String(length) + " bytes");
  return decoded;
}
function validateRequest(request: AuditRequest): void {
  equal(request.version, "v1", "request version");
  equal(request.circuit_set_id, circuitSetID, "request circuit_set_id");
  equal(request.circuit_id, circuitID, "request circuit_id");
  bytes(request.artifact_hash, "request artifact_hash", 32);
  equal(request.public_inputs.length, pi23Length, "request PI23 length");
  request.public_inputs.forEach((input, index) => bytes(input, "request PI23[" + String(index) + "]", 32));
  bytes(request.witness, "request witness");
}
function validateResponse(request: AuditRequest, response: AuditResponse): void {
  equal(response.version, "v1", "response version");
  equal(response.circuit_set_id, request.circuit_set_id, "response circuit_set_id");
  equal(response.circuit_id, request.circuit_id, "response circuit_id");
  if (!bytes(response.artifact_hash, "response artifact_hash", 32).equals(bytes(request.artifact_hash, "request artifact_hash", 32))) throw new Error("artifact hash binding mismatch");
  equal(response.public_inputs.length, pi23Length, "response PI23 length");
  response.public_inputs.forEach((input, index) => {
    if (!bytes(input, "response PI23[" + String(index) + "]", 32).equals(bytes(request.public_inputs[index], "request PI23[" + String(index) + "]", 32))) throw new Error("PI23 binding mismatch");
  });
  bytes(response.proof, "response proof");
}

async function prove(baseURL: string, token: string, request: AuditRequest): Promise<AuditResponse> {
  validateRequest(request);
  const endpoint = new URL(baseURL);
  const loopback = endpoint.hostname === "127.0.0.1" || endpoint.hostname === "localhost" || endpoint.hostname === "::1";
  if (endpoint.protocol !== "https:" && !(loopback && endpoint.protocol === "http:")) throw new Error("non-loopback prover URL must use HTTPS");
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 5_000);
  try {
    const response = await fetch(new URL(route, endpoint), { method: "POST", redirect: "error", headers: { "Content-Type": "application/json", Authorization: "Bearer " + token }, body: JSON.stringify(request), signal: controller.signal });
    const text = await response.text();
    if (!response.ok) throw new Error("prover request failed with status " + String(response.status));
    return JSON.parse(text) as AuditResponse;
  } finally {
    clearTimeout(timeout);
  }
}

async function readJSON(request: IncomingMessage): Promise<AuditRequest> {
  const chunks: Uint8Array[] = [];
  for await (const chunk of request) chunks.push(typeof chunk === "string" ? Buffer.from(chunk) : chunk);
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as AuditRequest;
}
function writeJSON(response: ServerResponse, status: number, body: unknown): void {
  response.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
  response.end(JSON.stringify(body));
}
async function mock(token: string): Promise<{ baseURL: string; close(): Promise<void> }> {
  const server = createServer(async (request, response) => {
    try {
      if (request.headers.authorization !== "Bearer " + token) return writeJSON(response, 401, { version: "v1", code: "unauthorized" });
      if (request.method !== "POST" || request.url !== route) return writeJSON(response, 404, { version: "v1", code: "not_found" });
      const body = await readJSON(request);
      validateRequest(body);
      writeJSON(response, 200, { version: "v1", circuit_set_id: body.circuit_set_id, circuit_id: body.circuit_id, artifact_hash: body.artifact_hash, public_inputs: body.public_inputs, proof: Buffer.alloc(128, 0x5a).toString("base64") } satisfies AuditResponse);
    } catch (error) {
      writeJSON(response, 400, { version: "v1", code: "invalid_request", message: error instanceof Error ? error.message : String(error) });
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("mock bind failed");
  return { baseURL: "http://127.0.0.1:" + String(address.port), close: () => new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve())) };
}
function fixture(): AuditRequest {
  return {
    version: "v1", circuit_set_id: circuitSetID, circuit_id: circuitID, artifact_hash: Buffer.alloc(32, 0xa5).toString("base64"),
    public_inputs: Array.from({ length: pi23Length }, (_, index) => Buffer.concat([Buffer.alloc(31), Buffer.from([index + 1])]).toString("base64")),
    // This is wire-shape data only; it is not a gnark witness or a valid proof fixture.
    witness: Buffer.from("mock witness is not a proving witness").toString("base64"),
  };
}
async function main(): Promise<void> {
  const token = randomBytes(16).toString("hex");
  const request = fixture();
  const server = await mock(token);
  try {
    const response = await prove(server.baseURL, token, request);
    validateResponse(request, response);
    let rejected = false;
    try { validateResponse(request, { ...response, circuit_id: "spend-audit-field-v1" }); } catch { rejected = true; }
    equal(rejected, true, "mismatched response rejection");
    console.log("Clairveil JS V2 audit-field prover HTTP client demo passed");
    console.log("- route: " + route + "; PI23 bindings checked: " + String(pi23Length));
  } finally {
    await server.close();
  }
}
await main();
