import { createHash, createHmac } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

interface TransferInput {
  amount: string;
  randomness_hex: string;
  spend_pubkey_hex: string;
  view_pubkey_hex: string;
  merkle_path: string[];
  merkle_path_helper: number[];
  note_hash_signature_hex: string;
  nullifier_hex: string;
}

interface TransferOutput {
  amount: string;
  randomness_hex: string;
  spend_pubkey_hex: string;
  view_pubkey_hex: string;
  commitment_hex: string;
}

interface PreparedTransferPayload {
  version: string;
  creator: string;
  root_hex: string;
  asset_id_hex: string;
  inputs: TransferInput[];
  outputs: TransferOutput[];
  cipher_text_hexes: string[];
  view_tag_hexes: string[];
  user_privacy_policy: number;
  user_disclosure_mode: number;
  user_disclosure_digest_hex?: string;
  user_disclosure_target_pubkey_hex?: string;
  user_disclosure_payload_hex?: string;
  audit_disclosure_digest_hex: string;
  audit_disclosure_target_pubkey_hex: string;
  audit_disclosure_payload_hex: string;
  self_view_disclosure_digest_hex?: string;
  self_view_disclosure_payload_hex?: string;
  payload_hash: string;
}

interface PreparedWithdrawProverPayload {
  version: string;
  root_hex: string;
  nullifier_hex: string;
  amount: string;
  asset_denom: string;
  asset_id_hex: string;
  recipient: string;
  recipient_bytes_hex: string;
  chain_id: string;
  expires_at_unix: number;
  note_randomness_hex: string;
  spend_pubkey_hex: string;
  view_pubkey_hex: string;
  merkle_path: string[];
  merkle_path_helper: number[];
  spend_note_hash_signature_hex: string;
  payload_hash: string;
}

interface PreparedWithdrawPayload {
  version: string;
  proof_hex: string;
  root_hex: string;
  nullifier_hex: string;
  amount: string;
  recipient: string;
  chain_id: string;
  expires_at_unix: number;
  payload_hash: string;
}

interface Proof {
  version: string;
  payload_hash: string;
  proof_hex: string;
}

interface ProverExampleBundle {
  schema_version: string;
  transfer: {
    request: {
      version: string;
      payload: PreparedTransferPayload;
    };
    response: {
      version: string;
      proof: Proof;
    };
  };
  withdraw: {
    validation_now_unix: number;
    request: {
      version: string;
      payload: PreparedWithdrawProverPayload;
    };
    response: {
      version: string;
      proof: Proof;
    };
  };
}

interface SendCapableReferenceFlow {
  schema_version: string;
  service: {
    transfer_path: string;
    withdraw_path: string;
  };
  transfer: {
    request_version: string;
    response_version: string;
    creator: string;
    payload_hash: string;
    proof_payload_hash: string;
    msg_creator: string;
  };
  withdraw: {
    request_version: string;
    response_version: string;
    payload_hash: string;
    proof_payload_hash: string;
    final_payload_hash: string;
    amount: string;
    asset_denom: string;
    recipient: string;
    chain_id: string;
    expires_at_unix: number;
  };
}

interface ProverHTTPAPIContract {
  schema_version: string;
  content_type: string;
  transfer_route: {
    method: string;
    path: string;
    request_version: string;
    response_version: string;
  };
  withdraw_route: {
    method: string;
    path: string;
    request_version: string;
    response_version: string;
  };
  error_response: {
    version: string;
    codes: string[];
  };
}

interface RelayWithdrawContract {
  schema_version: string;
  handoff_version: string;
  transport: string;
  request: {
    version: string;
    payload: PreparedWithdrawPayload;
  };
  relayer: {
    address: string;
  };
  expected_msg: {
    type_url: string;
    creator: string;
    proof_hex: string;
    root_hex: string;
    nullifier_hex: string;
    amount: string;
    recipient: string;
    chain_id: string;
    expires_at_unix: number;
  };
}

interface NoteReservationContract {
  version: number;
  active_reservation_statuses: string[];
  allowed_transitions: string[][];
  rejected_transitions: string[][];
  active_unique_key: string[];
  batch_reserve: {
    atomic: boolean;
    error_policy: string;
    lock_requirement: string;
  };
  nullifier_lookup_key: {
    algorithm: string;
    encoding: string;
    key_version_field: string;
    test_vectors: Array<{
      index_key_utf8: string;
      nullifier_utf8: string;
      lookup_key_hex: string;
    }>;
  };
  lease_transition_preconditions: {
    token_required_for: string[][];
    fields: string[];
    policy: string;
  };
  success_evidence_required: string[];
  batch_item_index_policy: string;
  operation_success_examples: Array<{
    name: string;
    nullifier_spent: boolean;
    evidence_matches_expected_values: boolean;
    note_status: string;
    operation_status: string;
  }>;
}

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../..");
const testdataDir = join(repoRoot, "x/privacy/client/sdk/conformance/testdata");
const schemaDir = join(repoRoot, "docs/schemas");
const maxShieldedAmount = (1n << 64n) - 1n;
const expectedActiveReservationStatuses = [
  "Reserved",
  "Proving",
  "ProofReady",
  "Submitted",
  "Unknown",
  "ManualReview",
];
const expectedAllowedTransitions = [
  ["Discovered", "Available"],
  ["Discovered", "Failed"],
  ["Available", "Reserved"],
  ["Reserved", "Proving"],
  ["Reserved", "Released"],
  ["Reserved", "ReplanRequired"],
  ["Reserved", "ManualReview"],
  ["Proving", "ProofReady"],
  ["Proving", "Reserved"],
  ["Proving", "ReplanRequired"],
  ["Proving", "ManualReview"],
  ["ProofReady", "Submitted"],
  ["ProofReady", "Unknown"],
  ["ProofReady", "ConfirmedSpent"],
  ["ProofReady", "ReplanRequired"],
  ["ProofReady", "ManualReview"],
  ["Submitted", "ConfirmedSpent"],
  ["Submitted", "Failed"],
  ["Submitted", "Unknown"],
  ["Submitted", "ReplanRequired"],
  ["Submitted", "ManualReview"],
  ["Unknown", "ConfirmedSpent"],
  ["Unknown", "Failed"],
  ["Unknown", "ReplanRequired"],
  ["Unknown", "ManualReview"],
  ["ManualReview", "ConfirmedSpent"],
  ["ManualReview", "Failed"],
  ["ManualReview", "Released"],
  ["ManualReview", "ReplanRequired"],
  ["Failed", "ReplanRequired"],
  ["Released", "Available"],
  ["ReplanRequired", "Reserved"],
  ["ReplanRequired", "Failed"],
  ["ReplanRequired", "ManualReview"],
];
const expectedRejectedTransitions = [
  ["Submitted", "Available"],
  ["Proving", "Released"],
  ["ProofReady", "Available"],
  ["ProofReady", "Released"],
  ["Unknown", "Available"],
  ["Unknown", "Submitted"],
  ["ManualReview", "Available"],
];
const expectedLeaseRequiredTransitions = [
  ["Reserved", "Proving"],
  ["Proving", "ProofReady"],
  ["ProofReady", "Submitted"],
  ["ProofReady", "Unknown"],
];
const expectedLeaseFields = ["lease_owner", "lease_token", "lease_until", "last_heartbeat_at"];
const expectedSuccessEvidenceRequired = [
  "tx_hash_or_tx_result",
  "expected_output_commitment",
  "expected_disclosure_digest",
  "expected_recipient_hash",
  "expected_amount_hash",
  "expected_denom",
  "batch_item_index",
  "batch_item_index_known",
];

type JsonSchema = {
  $ref?: string;
  type?: string | string[];
  const?: unknown;
  enum?: unknown[];
  pattern?: string;
  required?: string[];
  properties?: Record<string, JsonSchema>;
  additionalProperties?: boolean;
  allOf?: JsonSchema[];
  if?: JsonSchema;
  then?: JsonSchema;
  items?: JsonSchema;
  minItems?: number;
  maxItems?: number;
  minimum?: number;
  maximum?: number;
  minLength?: number;
};

type JsonSchemaDocument = JsonSchema & {
  $defs?: Record<string, JsonSchema>;
};

function readJSONFile<T>(fullPath: string): T {
  return JSON.parse(readFileSync(fullPath, "utf8")) as T;
}

function readFixture<T>(filename: string): T {
  return readJSONFile<T>(join(testdataDir, filename));
}

function sha256Hex(source: string): string {
  return createHash("sha256").update(source, "utf8").digest("hex");
}

function hmacSHA256Hex(keyUtf8: string, messageUtf8: string): string {
  return createHmac("sha256", Buffer.from(keyUtf8, "utf8"))
    .update(Buffer.from(messageUtf8, "utf8"))
    .digest("hex");
}

function assertEqual(actual: unknown, expected: unknown, label: string): void {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function assertStringArrayEqual(actual: string[], expected: string[], label: string): void {
  assertEqual(actual.length, expected.length, `${label} length`);
  for (const [index, expectedValue] of expected.entries()) {
    assertEqual(actual[index], expectedValue, `${label}[${index}]`);
  }
}

function transitionKey(transition: string[]): string {
  if (transition.length !== 2) {
    throw new Error(`transition: expected [from, to], got ${JSON.stringify(transition)}`);
  }
  return `${transition[0]}\u0000${transition[1]}`;
}

function assertTransitionSetEqual(actual: string[][], expected: string[][], label: string): void {
  const actualKeys = actual.map(transitionKey).sort();
  const expectedKeys = expected.map(transitionKey).sort();
  assertStringArrayEqual(actualKeys, expectedKeys, label);
}

function assertStartsWith(value: string, prefix: string, label: string): void {
  if (!value.startsWith(prefix)) {
    throw new Error(`${label}: expected prefix ${prefix}, got ${value}`);
  }
}

function assertHexLength(value: string, bytes: number, label: string): void {
  if (!/^[0-9a-f]*$/i.test(value) || value.length !== bytes * 2) {
    throw new Error(`${label}: expected ${bytes}-byte hex, got ${value}`);
  }
}

function assertHexStringNonEmpty(value: string, label: string): void {
  if (!/^[0-9a-f]+$/i.test(value)) {
    throw new Error(`${label}: expected non-empty hex, got ${value}`);
  }
}

function assertShieldedAmountString(value: string, label: string): void {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) {
    throw new Error(`${label}: expected a canonical non-negative decimal string, got ${value}`);
  }
  if (BigInt(value) > maxShieldedAmount) {
    throw new Error(`${label}: expected <= ${maxShieldedAmount.toString()}, got ${value}`);
  }
}

function assertSchema(condition: boolean, label: string, detail: string): void {
  if (!condition) {
    throw new Error(`${label}: ${detail}`);
  }
}

function schemaTypeOf(value: unknown): string {
  if (Array.isArray(value)) {
    return "array";
  }
  if (value === null) {
    return "null";
  }
  if (Number.isInteger(value)) {
    return "integer";
  }
  return typeof value;
}

function resolveSchemaRef(root: JsonSchemaDocument, ref: string): JsonSchema {
  const prefix = "#/$defs/";
  if (!ref.startsWith(prefix)) {
    throw new Error(`unsupported schema ref: ${ref}`);
  }
  const key = ref.slice(prefix.length);
  const resolved = root.$defs?.[key];
  if (!resolved) {
    throw new Error(`missing schema definition: ${key}`);
  }
  return resolved;
}

function validateJSONSchema(value: unknown, schema: JsonSchema, label: string, root: JsonSchemaDocument): void {
  if (schema.$ref) {
    validateJSONSchema(value, resolveSchemaRef(root, schema.$ref), label, root);
    return;
  }

  for (const [index, subschema] of (schema.allOf ?? []).entries()) {
    validateJSONSchema(value, subschema, `${label}.allOf[${index}]`, root);
  }
  if (schema.if && schema.then && schemaMatches(value, schema.if, root)) {
    validateJSONSchema(value, schema.then, `${label}.then`, root);
  }

  if ("const" in schema) {
    assertSchema(value === schema.const, label, `expected const ${String(schema.const)}, got ${String(value)}`);
  }
  if (schema.enum) {
    assertSchema(schema.enum.includes(value), label, `expected one of ${schema.enum.join(", ")}, got ${String(value)}`);
  }

  if (schema.type) {
    const allowedTypes = Array.isArray(schema.type) ? schema.type : [schema.type];
    const actualType = schemaTypeOf(value);
    const typeMatches = allowedTypes.some((expectedType) => {
      if (expectedType === "number") {
        return actualType === "number" || actualType === "integer";
      }
      return actualType === expectedType;
    });
    assertSchema(typeMatches, label, `expected type ${allowedTypes.join("|")}, got ${actualType}`);
  }

  if (typeof value === "string") {
    if (schema.pattern) {
      assertSchema(new RegExp(schema.pattern).test(value), label, `expected pattern ${schema.pattern}, got ${value}`);
    }
    if (schema.minLength !== undefined) {
      assertSchema(value.length >= schema.minLength, label, `expected minLength ${schema.minLength}, got ${value.length}`);
    }
  }

  if (typeof value === "number") {
    if (schema.minimum !== undefined) {
      assertSchema(value >= schema.minimum, label, `expected minimum ${schema.minimum}, got ${value}`);
    }
    if (schema.maximum !== undefined) {
      assertSchema(value <= schema.maximum, label, `expected maximum ${schema.maximum}, got ${value}`);
    }
  }

  if (Array.isArray(value)) {
    if (schema.minItems !== undefined) {
      assertSchema(value.length >= schema.minItems, label, `expected at least ${schema.minItems} items, got ${value.length}`);
    }
    if (schema.maxItems !== undefined) {
      assertSchema(value.length <= schema.maxItems, label, `expected at most ${schema.maxItems} items, got ${value.length}`);
    }
    if (schema.items) {
      value.forEach((item, index) => validateJSONSchema(item, schema.items as JsonSchema, `${label}[${index}]`, root));
    }
  }

  if (value !== null && typeof value === "object" && !Array.isArray(value)) {
    const objectValue = value as Record<string, unknown>;
    for (const requiredKey of schema.required ?? []) {
      assertSchema(Object.prototype.hasOwnProperty.call(objectValue, requiredKey), label, `missing required property ${requiredKey}`);
    }
    if (schema.additionalProperties === false && schema.properties) {
      for (const key of Object.keys(objectValue)) {
        assertSchema(Object.prototype.hasOwnProperty.call(schema.properties, key), label, `unexpected property ${key}`);
      }
    }
    for (const [key, propertySchema] of Object.entries(schema.properties ?? {})) {
      if (Object.prototype.hasOwnProperty.call(objectValue, key)) {
        validateJSONSchema(objectValue[key], propertySchema, `${label}.${key}`, root);
      }
    }
  }
}

function schemaMatches(value: unknown, schema: JsonSchema, root: JsonSchemaDocument): boolean {
  try {
    validateJSONSchema(value, schema, "schema condition", root);
    return true;
  } catch {
    return false;
  }
}

function validateFixtureSchemas(): void {
  const schema = readJSONFile<JsonSchemaDocument>(join(schemaDir, "clairveil-js-wallet-contract.schema.json"));
  const fixtureSchemas: Array<[string, string]> = [
    ["privacy_browser_signer_provider_contract.json", "browserSignerProviderContract"],
    ["privacy_prover_example_bundle.json", "proverExampleBundle"],
    ["privacy_prover_http_api_contract.json", "proverHttpApiContract"],
    ["privacy_note_reservation_contract.json", "noteReservationContract"],
    ["privacy_relay_withdraw_contract.json", "relayWithdrawContract"],
    ["privacy_send_capable_reference_flow.json", "sendCapableReferenceFlow"],
    ["privacy_wallet_golden_vectors.json", "walletGoldenVectors"],
    ["privacy_wallet_readonly_reference_bundle.json", "walletReadonlyReferenceBundle"],
  ];

  for (const [fixtureName, schemaName] of fixtureSchemas) {
    const fixture = readFixture<unknown>(fixtureName);
    validateJSONSchema(fixture, resolveSchemaRef(schema, `#/$defs/${schemaName}`), fixtureName, schema);
  }
  validateSchemaNegativeCases(schema);
}

function validateSchemaNegativeCases(schema: JsonSchemaDocument): void {
  const browserFixture = readFixture<Record<string, unknown>>("privacy_browser_signer_provider_contract.json");
  const browserSchema = resolveSchemaRef(schema, "#/$defs/browserSignerProviderContract");

  const malformedDeposit = deepClone(browserFixture);
  const depositEvents = scanEvents(malformedDeposit);
  depositEvents[0].outputs = [];
  assertSchemaRejects(malformedDeposit, browserSchema, "negative deposit scan event", schema, "expected at least 1 items");

  const malformedTransfer = deepClone(browserFixture);
  const transferEvents = scanEvents(malformedTransfer);
  transferEvents[1].outputs = transferEvents[1].outputs.slice(0, 1);
  assertSchemaRejects(malformedTransfer, browserSchema, "negative transfer scan event", schema, "expected at least 2 items");
}

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function scanEvents(fixture: Record<string, unknown>): Array<Record<string, unknown> & { outputs: unknown[] }> {
  const scanProvider = asRecord(fixture.scan_provider, "scan_provider");
  const response = asRecord(scanProvider.scan_events_response, "scan_events_response");
  const events = response.events;
  if (!Array.isArray(events)) {
    throw new Error("scan_events_response.events: expected array");
  }
  return events as Array<Record<string, unknown> & { outputs: unknown[] }>;
}

function asRecord(value: unknown, label: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label}: expected object`);
  }
  return value as Record<string, unknown>;
}

function assertSchemaRejects(value: unknown, schema: JsonSchema, label: string, root: JsonSchemaDocument, expectedMessage: string): void {
  try {
    validateJSONSchema(value, schema, label, root);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!message.includes(expectedMessage)) {
      throw new Error(`${label}: expected rejection containing ${expectedMessage}, got ${message}`);
    }
    return;
  }
  throw new Error(`${label}: expected schema validation to fail`);
}

function computePreparedTransferPayloadHash(payload: PreparedTransferPayload): string {
  const lines: string[] = [];
  const write = (value: string | number | undefined): void => {
    lines.push(String(value ?? ""));
  };
  const writeStringSlice = (values: string[]): void => {
    write(values.length);
    for (const value of values) {
      write(value);
    }
  };
  const writeUint32Slice = (values: number[]): void => {
    write(values.length);
    for (const value of values) {
      write(value);
    }
  };

  write(payload.version);
  write(payload.creator);
  write(payload.root_hex);
  write(payload.asset_id_hex);
  write(payload.user_privacy_policy);
  write(payload.user_disclosure_mode);
  write(payload.user_disclosure_digest_hex);
  write(payload.user_disclosure_target_pubkey_hex);
  write(payload.user_disclosure_payload_hex);
  write(payload.audit_disclosure_digest_hex);
  write(payload.audit_disclosure_target_pubkey_hex);
  write(payload.audit_disclosure_payload_hex);
  write(payload.self_view_disclosure_digest_hex);
  write(payload.self_view_disclosure_payload_hex);
  write(payload.inputs.length);
  for (const input of payload.inputs) {
    write(input.amount);
    write(input.randomness_hex);
    write(input.spend_pubkey_hex);
    write(input.view_pubkey_hex);
    writeStringSlice(input.merkle_path);
    writeUint32Slice(input.merkle_path_helper);
    write(input.note_hash_signature_hex);
    write(input.nullifier_hex);
  }
  write(payload.outputs.length);
  for (const output of payload.outputs) {
    write(output.amount);
    write(output.randomness_hex);
    write(output.spend_pubkey_hex);
    write(output.view_pubkey_hex);
    write(output.commitment_hex);
  }
  writeStringSlice(payload.cipher_text_hexes);
  writeStringSlice(payload.view_tag_hexes);

  return sha256Hex(`${lines.join("\n")}\n`);
}

function computePreparedWithdrawProverPayloadHash(payload: PreparedWithdrawProverPayload): string {
  const lines: string[] = [];
  const write = (value: string | number): void => {
    lines.push(String(value));
  };
  const writeStringSlice = (values: string[]): void => {
    write(values.length);
    for (const value of values) {
      write(value);
    }
  };
  const writeUint32Slice = (values: number[]): void => {
    write(values.length);
    for (const value of values) {
      write(value);
    }
  };

  write(payload.version);
  write(payload.root_hex);
  write(payload.nullifier_hex);
  write(payload.amount);
  write(payload.asset_denom);
  write(payload.asset_id_hex);
  write(payload.recipient);
  write(payload.recipient_bytes_hex);
  write(payload.chain_id);
  write(payload.expires_at_unix);
  write(payload.note_randomness_hex);
  write(payload.spend_pubkey_hex);
  write(payload.view_pubkey_hex);
  writeStringSlice(payload.merkle_path);
  writeUint32Slice(payload.merkle_path_helper);
  write(payload.spend_note_hash_signature_hex);

  return sha256Hex(`${lines.join("\n")}\n`);
}

function computePreparedWithdrawPayloadHash(input: {
  proofHex: string;
  rootHex: string;
  nullifierHex: string;
  amount: string;
  recipient: string;
  chainID: string;
  version: string;
  expiresAtUnix: number;
}): string {
  return sha256Hex([
    input.version,
    input.proofHex,
    input.rootHex,
    input.nullifierHex,
    input.amount,
    input.recipient,
    input.chainID,
    String(input.expiresAtUnix),
  ].join("\n"));
}

function computePreparedWithdrawPayloadHashFromPayload(payload: PreparedWithdrawPayload): string {
  return computePreparedWithdrawPayloadHash({
    proofHex: payload.proof_hex,
    rootHex: payload.root_hex,
    nullifierHex: payload.nullifier_hex,
    amount: payload.amount,
    recipient: payload.recipient,
    chainID: payload.chain_id,
    version: payload.version,
    expiresAtUnix: payload.expires_at_unix,
  });
}

function validateWalletFacingPrefixes(): void {
  const unexpectedAddressPattern = /(?<![a-z0-9])([a-z]{2,12}1[0-9a-z]{20,})/g;
  const allowedPrefixes = ["clair1", "clairs1"];
  for (const filename of readdirSync(testdataDir)) {
    if (!filename.endsWith(".json")) {
      continue;
    }
    const body = readFileSync(join(testdataDir, filename), "utf8");
    for (const match of body.matchAll(unexpectedAddressPattern)) {
      const address = match[1];
      if (!allowedPrefixes.some((prefix) => address.startsWith(prefix))) {
        throw new Error(`${filename}: unexpected wallet-facing address prefix in ${address}`);
      }
    }
  }
}

function validateProverExampleBundle(bundle: ProverExampleBundle): void {
  assertEqual(bundle.schema_version, "v1", "prover bundle schema_version");
  assertEqual(bundle.transfer.request.version, "v1", "transfer request version");
  assertEqual(bundle.transfer.response.version, "v1", "transfer response version");
  assertEqual(bundle.withdraw.request.version, "v1", "withdraw request version");
  assertEqual(bundle.withdraw.response.version, "v1", "withdraw response version");

  const transferPayload = bundle.transfer.request.payload;
  const transferHash = computePreparedTransferPayloadHash(transferPayload);
  assertEqual(transferPayload.version, "v3", "transfer payload version");
  assertStartsWith(transferPayload.creator, "clair1", "transfer creator");
  assertHexLength(transferPayload.self_view_disclosure_digest_hex ?? "", 32, "transfer self-view disclosure digest");
  assertHexStringNonEmpty(transferPayload.self_view_disclosure_payload_hex ?? "", "transfer self-view disclosure payload");
  transferPayload.inputs.forEach((input, index) => {
    assertShieldedAmountString(input.amount, `transfer input ${index} amount`);
  });
  transferPayload.outputs.forEach((output, index) => {
    assertShieldedAmountString(output.amount, `transfer output ${index} amount`);
  });
  assertEqual(transferPayload.view_tag_hexes.length, 2, "transfer view tag count");
  transferPayload.view_tag_hexes.forEach((viewTag, index) => {
    assertHexLength(viewTag, 2, `transfer view tag ${index}`);
  });
  assertEqual(transferPayload.payload_hash, transferHash, "transfer payload_hash");
  assertEqual(bundle.transfer.response.proof.payload_hash, transferHash, "transfer proof payload_hash");

  const withdrawPayload = bundle.withdraw.request.payload;
  const withdrawHash = computePreparedWithdrawProverPayloadHash(withdrawPayload);
  assertShieldedAmountString(withdrawPayload.amount, "withdraw amount");
  assertStartsWith(withdrawPayload.recipient, "clair1", "withdraw recipient");
  assertHexLength(withdrawPayload.recipient_bytes_hex, 20, "withdraw recipient_bytes_hex");
  assertEqual(withdrawPayload.payload_hash, withdrawHash, "withdraw prover payload_hash");
  assertEqual(bundle.withdraw.response.proof.payload_hash, withdrawHash, "withdraw proof payload_hash");
}

function validateSendCapableReferenceFlow(
  flow: SendCapableReferenceFlow,
  bundle: ProverExampleBundle,
): void {
  assertEqual(flow.schema_version, "v1", "send-capable schema_version");
  assertEqual(flow.service.transfer_path, "/v1/prover/transfer", "transfer prover path");
  assertEqual(flow.service.withdraw_path, "/v1/prover/withdraw", "withdraw prover path");

  const transferPayload = bundle.transfer.request.payload;
  assertEqual(flow.transfer.creator, transferPayload.creator, "send-capable transfer creator");
  assertEqual(flow.transfer.msg_creator, transferPayload.creator, "send-capable transfer msg_creator");
  assertEqual(flow.transfer.payload_hash, transferPayload.payload_hash, "send-capable transfer payload_hash");
  assertEqual(flow.transfer.proof_payload_hash, bundle.transfer.response.proof.payload_hash, "send-capable transfer proof hash");

  const withdrawPayload = bundle.withdraw.request.payload;
  const finalWithdrawHash = computePreparedWithdrawPayloadHash({
    proofHex: bundle.withdraw.response.proof.proof_hex,
    rootHex: withdrawPayload.root_hex,
    nullifierHex: withdrawPayload.nullifier_hex,
    amount: `${withdrawPayload.amount}${withdrawPayload.asset_denom}`,
    recipient: withdrawPayload.recipient,
    chainID: withdrawPayload.chain_id,
    version: "v1",
    expiresAtUnix: withdrawPayload.expires_at_unix,
  });

  assertEqual(flow.withdraw.payload_hash, withdrawPayload.payload_hash, "send-capable withdraw payload_hash");
  assertEqual(flow.withdraw.proof_payload_hash, bundle.withdraw.response.proof.payload_hash, "send-capable withdraw proof hash");
  assertEqual(flow.withdraw.final_payload_hash, finalWithdrawHash, "send-capable final withdraw hash");
  assertEqual(flow.withdraw.recipient, withdrawPayload.recipient, "send-capable withdraw recipient");
  assertEqual(flow.withdraw.amount, `${withdrawPayload.amount}${withdrawPayload.asset_denom}`, "send-capable withdraw amount");
}

function validateRelayWithdrawContract(
  contract: RelayWithdrawContract,
  flow: SendCapableReferenceFlow,
): void {
  assertEqual(contract.schema_version, "v1", "relay-withdraw schema_version");
  assertEqual(contract.handoff_version, "v1", "relay-withdraw handoff_version");
  assertEqual(contract.transport, "transport-agnostic", "relay-withdraw transport");
  assertEqual(contract.request.version, "v1", "relay-withdraw request version");

  const payload = contract.request.payload;
  const expectedMsg = contract.expected_msg;
  const finalWithdrawHash = computePreparedWithdrawPayloadHashFromPayload(payload);

  assertEqual(payload.payload_hash, finalWithdrawHash, "relay-withdraw payload_hash");
  assertEqual(payload.payload_hash, flow.withdraw.final_payload_hash, "relay-withdraw send-flow final payload hash");
  assertStartsWith(payload.recipient, "clair1", "relay-withdraw recipient");
  assertStartsWith(contract.relayer.address, "clair1", "relay-withdraw relayer");
  assertHexLength(payload.root_hex, 32, "relay-withdraw root_hex");
  assertHexLength(payload.nullifier_hex, 32, "relay-withdraw nullifier_hex");

  assertEqual(expectedMsg.type_url, "/clairveil.privacy.v1.MsgWithdraw", "relay-withdraw MsgWithdraw type URL");
  assertEqual(expectedMsg.creator, contract.relayer.address, "relay-withdraw MsgWithdraw creator");
  assertEqual(expectedMsg.proof_hex, payload.proof_hex, "relay-withdraw MsgWithdraw proof");
  assertEqual(expectedMsg.root_hex, payload.root_hex, "relay-withdraw MsgWithdraw root");
  assertEqual(expectedMsg.nullifier_hex, payload.nullifier_hex, "relay-withdraw MsgWithdraw nullifier");
  assertEqual(expectedMsg.amount, payload.amount, "relay-withdraw MsgWithdraw amount");
  assertEqual(expectedMsg.recipient, payload.recipient, "relay-withdraw MsgWithdraw recipient");
  assertEqual(expectedMsg.chain_id, payload.chain_id, "relay-withdraw MsgWithdraw chain_id");
  assertEqual(expectedMsg.expires_at_unix, payload.expires_at_unix, "relay-withdraw MsgWithdraw expires_at_unix");
}

function validateProverHTTPAPIContract(contract: ProverHTTPAPIContract): void {
  assertEqual(contract.schema_version, "v1", "prover HTTP schema_version");
  assertEqual(contract.content_type, "application/json", "prover HTTP content_type");
  assertEqual(contract.transfer_route.method, "POST", "transfer HTTP method");
  assertEqual(contract.transfer_route.path, "/v1/prover/transfer", "transfer HTTP path");
  assertEqual(contract.transfer_route.request_version, "v1", "transfer HTTP request version");
  assertEqual(contract.transfer_route.response_version, "v1", "transfer HTTP response version");
  assertEqual(contract.withdraw_route.method, "POST", "withdraw HTTP method");
  assertEqual(contract.withdraw_route.path, "/v1/prover/withdraw", "withdraw HTTP path");
  assertEqual(contract.withdraw_route.request_version, "v1", "withdraw HTTP request version");
  assertEqual(contract.withdraw_route.response_version, "v1", "withdraw HTTP response version");

  const requiredErrorCodes = [
    "invalid_request",
    "method_not_allowed",
    "not_found",
    "unauthorized",
    "unavailable",
    "proof_failed",
  ];
  assertEqual(contract.error_response.version, "v1", "prover HTTP error version");
  assertEqual(contract.error_response.codes.length, requiredErrorCodes.length, "prover HTTP error code count");
  for (const code of requiredErrorCodes) {
    if (!contract.error_response.codes.includes(code)) {
      throw new Error(`prover HTTP error codes: missing ${code}`);
    }
  }
}

function validateNoteReservationContract(contract: NoteReservationContract): void {
  assertEqual(contract.version, 1, "note reservation version");
  assertStringArrayEqual(
    contract.active_reservation_statuses,
    expectedActiveReservationStatuses,
    "note reservation active statuses",
  );
  assertTransitionSetEqual(contract.allowed_transitions, expectedAllowedTransitions, "note reservation allowed transitions");
  assertTransitionSetEqual(contract.rejected_transitions, expectedRejectedTransitions, "note reservation rejected transitions");
  assertStringArrayEqual(
    contract.active_unique_key,
    ["owner_key_id", "nullifier_lookup_key"],
    "note reservation active unique key",
  );

  assertEqual(contract.batch_reserve.atomic, true, "note reservation batch reserve atomic");
  assertSchema(
    contract.batch_reserve.error_policy.trim().length > 0,
    "note reservation batch reserve error_policy",
    "expected non-empty policy",
  );
  assertSchema(
    contract.batch_reserve.lock_requirement.trim().length > 0,
    "note reservation batch reserve lock_requirement",
    "expected non-empty lock requirement",
  );

  assertEqual(contract.nullifier_lookup_key.algorithm, "HMAC-SHA256", "note reservation lookup algorithm");
  assertEqual(contract.nullifier_lookup_key.encoding, "hex", "note reservation lookup encoding");
  assertEqual(
    contract.nullifier_lookup_key.key_version_field,
    "nullifier_lookup_key_id",
    "note reservation lookup key version field",
  );
  assertSchema(
    contract.nullifier_lookup_key.test_vectors.length > 0,
    "note reservation lookup vectors",
    "expected at least one vector",
  );
  for (const [index, vector] of contract.nullifier_lookup_key.test_vectors.entries()) {
    assertEqual(
      vector.lookup_key_hex,
      hmacSHA256Hex(vector.index_key_utf8, vector.nullifier_utf8),
      `note reservation lookup vector ${index}`,
    );
  }

  assertTransitionSetEqual(
    contract.lease_transition_preconditions.token_required_for,
    expectedLeaseRequiredTransitions,
    "note reservation lease-required transitions",
  );
  assertStringArrayEqual(contract.lease_transition_preconditions.fields, expectedLeaseFields, "note reservation lease fields");
  assertSchema(
    contract.lease_transition_preconditions.policy.trim().length > 0,
    "note reservation lease policy",
    "expected non-empty policy",
  );
  assertStringArrayEqual(
    contract.success_evidence_required,
    expectedSuccessEvidenceRequired,
    "note reservation success evidence",
  );
  assertSchema(
    contract.batch_item_index_policy.trim().length > 0,
    "note reservation batch item index policy",
    "expected non-empty batch item index policy",
  );

  assertEqual(contract.operation_success_examples.length, 2, "note reservation success example count");
  const successExample = contract.operation_success_examples.find((example) => example.evidence_matches_expected_values);
  const conflictExample = contract.operation_success_examples.find((example) => !example.evidence_matches_expected_values);
  assertSchema(Boolean(successExample), "note reservation success example", "expected matching-evidence example");
  assertSchema(Boolean(conflictExample), "note reservation conflict example", "expected mismatch-evidence example");
  if (successExample) {
    assertEqual(successExample.nullifier_spent, true, "note reservation success example nullifier_spent");
    assertEqual(successExample.note_status, "ConfirmedSpent", "note reservation success example note status");
    assertEqual(successExample.operation_status, "Succeeded", "note reservation success example operation status");
  }
  if (conflictExample) {
    assertEqual(conflictExample.nullifier_spent, true, "note reservation conflict example nullifier_spent");
    assertEqual(conflictExample.note_status, "ConfirmedSpent", "note reservation conflict example note status");
    assertEqual(conflictExample.operation_status, "ConflictSpent", "note reservation conflict example operation status");
  }
}

function validateWalletFixtures(): void {
  const golden = readFixture<Record<string, any>>("privacy_wallet_golden_vectors.json");
  const readonly = readFixture<Record<string, any>>("privacy_wallet_readonly_reference_bundle.json");
  const browser = readFixture<Record<string, any>>("privacy_browser_signer_provider_contract.json");

  assertStartsWith(golden.sender_root_seed.address, "clair1", "golden sender transparent address");
  assertStartsWith(golden.recipient_root_seed.address, "clair1", "golden recipient transparent address");
  assertStartsWith(golden.sender.shielded_address, "clairs1", "golden sender shielded address");
  assertStartsWith(golden.recipient.shielded_address, "clairs1", "golden recipient shielded address");
  assertEqual(golden.note.denom, "uclair", "golden note denom");
  assertShieldedAmountString(golden.note.amount, "golden note amount");

  assertStartsWith(readonly.sender.transparent_address, "clair1", "readonly sender transparent address");
  assertStartsWith(readonly.recipient.transparent_address, "clair1", "readonly recipient transparent address");
  assertStartsWith(readonly.sender.show_address.address, "clairs1", "readonly sender shielded address");
  assertStartsWith(readonly.recipient.show_address.address, "clairs1", "readonly recipient shielded address");
  assertEqual(readonly.disclosure.asset_denom, "uclair", "readonly disclosure denom");
  assertShieldedAmountString(readonly.disclosure.amount, "readonly disclosure amount");
  for (const [index, note] of readonly.scan.deposit_found.entries()) {
    assertShieldedAmountString(note.amount, `readonly deposit note ${index} amount`);
  }
  for (const [index, note] of readonly.scan.transfer_found.entries()) {
    assertShieldedAmountString(note.amount, `readonly transfer note ${index} amount`);
  }

  assertStartsWith(browser.root_signer.get_account_response.transparent_address, "clair1", "browser transparent address");
  assertStartsWith(browser.root_signer.expected_derived.shielded_address, "clairs1", "browser shielded address");
}

function main(): void {
  const proverBundle = readFixture<ProverExampleBundle>("privacy_prover_example_bundle.json");
  const sendFlow = readFixture<SendCapableReferenceFlow>("privacy_send_capable_reference_flow.json");
  const proverHTTPContract = readFixture<ProverHTTPAPIContract>("privacy_prover_http_api_contract.json");
  const relayWithdrawContract = readFixture<RelayWithdrawContract>("privacy_relay_withdraw_contract.json");
  const noteReservationContract = readFixture<NoteReservationContract>("privacy_note_reservation_contract.json");

  validateFixtureSchemas();
  validateWalletFacingPrefixes();
  validateProverExampleBundle(proverBundle);
  validateSendCapableReferenceFlow(sendFlow, proverBundle);
  validateRelayWithdrawContract(relayWithdrawContract, sendFlow);
  validateProverHTTPAPIContract(proverHTTPContract);
  validateNoteReservationContract(noteReservationContract);
  validateWalletFixtures();

  console.log("Clairveil JS SDK fixture validator passed");
  console.log(`- transfer payload hash: ${proverBundle.transfer.request.payload.payload_hash}`);
  console.log(`- withdraw prover payload hash: ${proverBundle.withdraw.request.payload.payload_hash}`);
  console.log(`- final withdraw payload hash: ${sendFlow.withdraw.final_payload_hash}`);
  console.log(`- relay withdraw creator: ${relayWithdrawContract.expected_msg.creator}`);
}

main();
