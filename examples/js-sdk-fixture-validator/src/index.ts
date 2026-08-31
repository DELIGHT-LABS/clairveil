import { createHash, createHmac } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fromBech32, toBech32 } from "@cosmjs/encoding";

interface TransferInput {
  amount: string;
  randomness_hex: string;
  spend_pubkey_hex: string;
  view_pubkey_hex: string;
  merkle_path: string[];
  merkle_path_helper: number[];
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
  chain_id: string;
  expires_at_unix: number;
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
  user_disclosure_blinding_hex?: string;
  full_disclosure_blinding_hex: string;
  owner_signature_hex: string;
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
  spend_intent_signature_hex: string;
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
    retryable_codes: string[];
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

export interface NoteReservationContract {
  version: number;
  fixture_migration: {
    from_version: number;
    to_version: number;
    downstream_action: string;
  };
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
  operation_hash_test_vectors: Array<{
    recipient: string;
    recipient_hash: string;
    denom: string;
    amount: string;
    amount_hash: string;
  }>;
  operation_hash_rejection_vectors: Array<{
    name: string;
    recipient: string;
    denom: string;
    amount: string;
    reject_hash: "recipient" | "amount";
  }>;
  lease_transition_preconditions: {
    token_required_for: string[][];
    recovery_without_token_after_expiry_for: string[][];
    fields: string[];
    policy: string;
  };
  transition_evidence_preconditions: Array<{
    name: string;
    transition: string[];
    required_evidence: string[];
    positive: Record<string, boolean>;
    negative: Record<string, boolean>;
  }>;
  manual_review_resolution: {
    required_evidence: string[];
    positive: Record<string, unknown>;
    negative: Record<string, unknown>;
  };
  relay_handoff: {
    status: string;
    lease_must_remain: boolean;
    record_requires: string[];
    proof_discard_after_handoff: string;
    write_once_evidence: string[];
    positive: Record<string, unknown>;
    negative: Record<string, unknown>;
    negative_vectors: Array<{
      name: string;
      payload_hash_matches: boolean;
      all_reservations_proof_ready: boolean;
      operation_reservation_set_exact: boolean;
    }>;
  };
  initial_state_preconditions: {
    reservation_status: string;
    operation_status: string;
    forbidden_reservation_evidence: string[];
    forbidden_operation_evidence: string[];
    positive: Record<string, boolean>;
    negative: Record<string, boolean>;
  };
  fail_closed_runtime_policy: {
    nullifier_spent_evidence: {
      spent_value: boolean;
      unspent_value: boolean;
      other_values: string;
    };
    relay_submission: {
      chain_time_source: string;
      chain_time_required: boolean;
      recheck_immediately_before_broadcast: boolean;
      on_unavailable: string;
    };
	  heartbeat: {
	    coverage: string[];
	    await_in_flight_before_stop: boolean;
	  };
	  broadcast_boundary: {
	    durable_attempt_before_external_call: boolean;
	    retry_blocked_until_reconciled: boolean;
	  };
	};
  evidence_immutability: {
    write_once_fields: string[];
    monotonic_fields: string[];
    negative: Record<string, unknown>;
    mutation_rejection_vectors: Array<{
      field: string;
      original: unknown;
      mutation: unknown;
    }>;
  };
  spent_sibling_quarantine: {
    match_fields: string[];
    target_status: string;
    positive: Record<string, number>;
    negative: Record<string, number>;
  };
  success_evidence_required: string[];
  batch_item_index_policy: string;
  operation_identity_evidence: {
    required: string;
    vectors: Array<{
      name: string;
      stored_tx_hash?: string;
      stored_tx_bytes_hash?: string;
      stored_sign_doc_hash?: string;
      tx_result: {
        code?: number;
        txhash?: string;
        tx_bytes_hash?: string;
        sign_doc_hash?: string;
      };
      operation_status: string;
    }>;
  };
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
  ["Proving", "Reserved"],
  ["Proving", "ReplanRequired"],
  ["Proving", "ManualReview"],
  ["ProofReady", "Submitted"],
  ["ProofReady", "Unknown"],
  ["ProofReady", "ReplanRequired"],
  ["ProofReady", "ManualReview"],
];
const expectedLeaseExpiryRecoveryTransitions = [
  ["Proving", "ReplanRequired"],
  ["Proving", "ManualReview"],
  ["ProofReady", "ManualReview"],
];
const expectedLeaseFields = ["lease_owner", "lease_token", "lease_until", "last_heartbeat_at"];
const expectedSuccessEvidenceRequired = [
  "matching_persisted_tx_identity",
  "expected_output_commitment",
  "expected_disclosure_digest",
  "expected_recipient_hash",
  "expected_amount_hash",
  "expected_denom",
  "batch_item_index",
  "batch_item_index_known",
];

export type JsonSchema = {
  $ref?: string;
  type?: string | string[];
  const?: unknown;
  enum?: unknown[];
  pattern?: string;
  required?: string[];
  properties?: Record<string, JsonSchema>;
  additionalProperties?: boolean;
  allOf?: JsonSchema[];
  anyOf?: JsonSchema[];
  oneOf?: JsonSchema[];
  if?: JsonSchema;
  then?: JsonSchema;
  items?: JsonSchema;
  minItems?: number;
  maxItems?: number;
  minimum?: number;
  maximum?: number;
  minLength?: number;
};

export type JsonSchemaDocument = JsonSchema & {
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

const fieldModulus = 21888242871839275222246405745257275088548364400416034343698204186575808495617n;
const curveOrder = 2736030358979909402780800718157159386076813972158567259200215660948447373041n;
const curveD = 12181644023421730124874158521699555681764249180949974110617291017600649128846n;
const fieldHalf = (fieldModulus - 1n) / 2n;

function mod(value: bigint): bigint {
  const result = value % fieldModulus;
  return result >= 0n ? result : result + fieldModulus;
}

function modPow(base: bigint, exponent: bigint): bigint {
  let result = 1n;
  let value = mod(base);
  let power = exponent;
  while (power > 0n) {
    if (power & 1n) result = (result * value) % fieldModulus;
    value = (value * value) % fieldModulus;
    power >>= 1n;
  }
  return result;
}

function modInv(value: bigint): bigint {
  const normalized = mod(value);
  if (normalized === 0n) throw new Error("point denominator is zero");
  return modPow(normalized, fieldModulus - 2n);
}

function modSqrt(value: bigint): bigint {
  const n = mod(value);
  if (n === 0n) return 0n;
  if (modPow(n, (fieldModulus - 1n) / 2n) !== 1n) {
    throw new Error("point is not on the Clairveil curve");
  }
  let q = fieldModulus - 1n;
  let s = 0n;
  while ((q & 1n) === 0n) {
    q >>= 1n;
    s += 1n;
  }
  let z = 2n;
  while (modPow(z, (fieldModulus - 1n) / 2n) !== fieldModulus - 1n) z += 1n;
  let c = modPow(z, q);
  let x = modPow(n, (q + 1n) / 2n);
  let t = modPow(n, q);
  let m = s;
  while (t !== 1n) {
    let i = 1n;
    let candidate = (t * t) % fieldModulus;
    while (candidate !== 1n) {
      candidate = (candidate * candidate) % fieldModulus;
      i += 1n;
      if (i >= m) throw new Error("field square root failed");
    }
    const b = modPow(c, 1n << (m - i - 1n));
    x = (x * b) % fieldModulus;
    const b2 = (b * b) % fieldModulus;
    t = (t * b2) % fieldModulus;
    c = b2;
    m = i;
  }
  return x;
}

function bytesToBigIntLE(bytes: Uint8Array): bigint {
  const hex = Buffer.from(bytes).reverse().toString("hex");
  return hex ? BigInt(`0x${hex}`) : 0n;
}

function bigIntToBytesLE(value: bigint): Uint8Array {
  const hex = mod(value).toString(16).padStart(64, "0");
  return Uint8Array.from(Buffer.from(hex, "hex")).reverse();
}

type CurvePoint = { x: bigint; y: bigint };

function pointAdd(left: CurvePoint, right: CurvePoint): CurvePoint {
  const xProduct = mod(left.x * right.x);
  const yProduct = mod(left.y * right.y);
  const cross = mod(curveD * xProduct * yProduct);
  return {
    x: mod((left.x * right.y + left.y * right.x) * modInv(1n + cross)),
    y: mod((yProduct + xProduct) * modInv(1n - cross)),
  };
}

function scalarMultiply(point: CurvePoint, scalar: bigint): CurvePoint {
  let result: CurvePoint = { x: 0n, y: 1n };
  let addend = point;
  let remaining = scalar;
  while (remaining > 0n) {
    if (remaining & 1n) result = pointAdd(result, addend);
    addend = pointAdd(addend, addend);
    remaining >>= 1n;
  }
  return result;
}

export function canonicalCompressedPoint(input: Uint8Array): Uint8Array {
  assertSchema(input.length === 32, "shielded recipient point", "expected 32-byte point");
  const yBytes = Uint8Array.from(input);
  const sign = (yBytes[31] & 0x80) !== 0;
  yBytes[31] &= 0x7f;
  const y = bytesToBigIntLE(yBytes);
  assertSchema(y < fieldModulus, "shielded recipient point", "non-canonical y coordinate");
  const y2 = (y * y) % fieldModulus;
  const numerator = mod(1n - y2);
  const denominator = mod(-1n - curveD * y2);
  const ratio = mod(numerator * modInv(denominator));
  let x = modSqrt(ratio);
  if ((x > fieldHalf) !== sign) x = mod(-x);
  assertSchema(!(x === 0n && y === 1n), "shielded recipient point", "identity is not allowed");
  const subgroupCheck = scalarMultiply({ x, y }, curveOrder);
  assertSchema(
    subgroupCheck.x === 0n && subgroupCheck.y === 1n,
    "shielded recipient point",
    "point is not in the prime-order subgroup",
  );
  const encoded = bigIntToBytesLE(y);
  if (x > fieldHalf) encoded[31] |= 0x80;
  return encoded;
}

function validCosmosDenom(value: string): boolean {
  return /^[A-Za-z][A-Za-z0-9/:._-]{2,127}$/.test(value);
}

function canonicalShieldedAddress(value: string): string {
  const decoded = fromBech32(value.trim(), 200);
  assertSchema(decoded.prefix === "clairs", "shielded recipient", "expected clairs prefix");
  assertSchema(decoded.data.length === 64, "shielded recipient", "expected 64-byte payload");
  const canonical = new Uint8Array(64);
  canonical.set(canonicalCompressedPoint(decoded.data.slice(0, 32)), 0);
  canonical.set(canonicalCompressedPoint(decoded.data.slice(32, 64)), 32);
  return toBech32("clairs", canonical, 200);
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

export function validateJSONSchema(value: unknown, schema: JsonSchema, label: string, root: JsonSchemaDocument): void {
  if (schema.$ref) {
    validateJSONSchema(value, resolveSchemaRef(root, schema.$ref), label, root);
    return;
  }

  for (const [index, subschema] of (schema.allOf ?? []).entries()) {
    validateJSONSchema(value, subschema, `${label}.allOf[${index}]`, root);
  }
  if (schema.anyOf) {
    assertSchema(
      schema.anyOf.some((subschema) => schemaMatches(value, subschema, root)),
      label,
      "expected at least one anyOf schema to match",
    );
  }
  if (schema.oneOf) {
    const matches = schema.oneOf.filter((subschema) => schemaMatches(value, subschema, root)).length;
    assertSchema(matches === 1, label, `expected exactly one oneOf schema to match, got ${matches}`);
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
  write(payload.chain_id);
  write(payload.expires_at_unix);
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
  write(payload.user_disclosure_blinding_hex);
  write(payload.full_disclosure_blinding_hex);
  write(payload.owner_signature_hex);
  write(payload.inputs.length);
  for (const input of payload.inputs) {
    write(input.amount);
    write(input.randomness_hex);
    write(input.spend_pubkey_hex);
    write(input.view_pubkey_hex);
    writeStringSlice(input.merkle_path);
    writeUint32Slice(input.merkle_path_helper);
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
  write(payload.spend_intent_signature_hex);

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

  const visit = (filename: string, value: unknown): void => {
    if (typeof value === "string") {
      // Canonical fixed payloads and encrypted envelopes are represented as
      // hex in JSON fixtures. Scanning their random bytes as text creates
      // false Bech32 matches, so only non-hex strings are address-scanned.
      if (/^[0-9a-fA-F]+$/.test(value) && value.length % 2 === 0) {
        return;
      }
      for (const match of value.matchAll(unexpectedAddressPattern)) {
        const address = match[1];
        if (!allowedPrefixes.some((prefix) => address.startsWith(prefix))) {
          throw new Error(`${filename}: unexpected wallet-facing address prefix in ${address}`);
        }
      }
      return;
    }
    if (Array.isArray(value)) {
      for (const item of value) {
        visit(filename, item);
      }
      return;
    }
    if (value !== null && typeof value === "object") {
      for (const item of Object.values(value as Record<string, unknown>)) {
        visit(filename, item);
      }
    }
  };

  for (const filename of readdirSync(testdataDir)) {
    if (!filename.endsWith(".json")) {
      continue;
    }
    visit(filename, JSON.parse(readFileSync(join(testdataDir, filename), "utf8")) as unknown);
  }
}

function validateProverExampleBundle(bundle: ProverExampleBundle): void {
  assertEqual(bundle.schema_version, "v2", "prover bundle schema_version");
  assertEqual(bundle.transfer.request.version, "v2", "transfer request version");
  assertEqual(bundle.transfer.response.version, "v2", "transfer response version");
  assertEqual(bundle.withdraw.request.version, "v2", "withdraw request version");
  assertEqual(bundle.withdraw.response.version, "v2", "withdraw response version");

  const transferPayload = bundle.transfer.request.payload;
  const transferHash = computePreparedTransferPayloadHash(transferPayload);
  assertEqual(transferPayload.version, "v5", "transfer payload version");
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
  assertHexLength(transferPayload.owner_signature_hex, 64, "transfer owner intent signature");
  assertEqual(bundle.transfer.response.proof.payload_hash, transferHash, "transfer proof payload_hash");

  const withdrawPayload = bundle.withdraw.request.payload;
  const withdrawHash = computePreparedWithdrawProverPayloadHash(withdrawPayload);
  assertShieldedAmountString(withdrawPayload.amount, "withdraw amount");
  assertStartsWith(withdrawPayload.recipient, "clair1", "withdraw recipient");
  assertHexLength(withdrawPayload.recipient_bytes_hex, 20, "withdraw recipient_bytes_hex");
  assertEqual(withdrawPayload.payload_hash, withdrawHash, "withdraw prover payload_hash");
  assertHexLength(withdrawPayload.spend_intent_signature_hex, 64, "withdraw spend intent signature");
  assertEqual(bundle.withdraw.response.proof.payload_hash, withdrawHash, "withdraw proof payload_hash");
}

function validateSendCapableReferenceFlow(
  flow: SendCapableReferenceFlow,
  bundle: ProverExampleBundle,
): void {
  assertEqual(flow.schema_version, "v2", "send-capable schema_version");
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
    version: "v2",
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
  assertEqual(contract.schema_version, "v2", "relay-withdraw schema_version");
  assertEqual(contract.handoff_version, "v2", "relay-withdraw handoff_version");
  assertEqual(contract.transport, "transport-agnostic", "relay-withdraw transport");
  assertEqual(contract.request.version, "v2", "relay-withdraw request version");

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
  assertEqual(contract.schema_version, "v2", "prover HTTP schema_version");
  assertEqual(contract.content_type, "application/json", "prover HTTP content_type");
  assertEqual(contract.transfer_route.method, "POST", "transfer HTTP method");
  assertEqual(contract.transfer_route.path, "/v1/prover/transfer", "transfer HTTP path");
  assertEqual(contract.transfer_route.request_version, "v2", "transfer HTTP request version");
  assertEqual(contract.transfer_route.response_version, "v2", "transfer HTTP response version");
  assertEqual(contract.withdraw_route.method, "POST", "withdraw HTTP method");
  assertEqual(contract.withdraw_route.path, "/v1/prover/withdraw", "withdraw HTTP path");
  assertEqual(contract.withdraw_route.request_version, "v2", "withdraw HTTP request version");
  assertEqual(contract.withdraw_route.response_version, "v2", "withdraw HTTP response version");

  const requiredErrorCodes = [
    "invalid_request",
    "method_not_allowed",
    "not_found",
    "unauthorized",
    "unavailable",
    "proof_failed",
    "busy",
  ];
  assertEqual(contract.error_response.version, "v1", "prover HTTP error version");
  assertEqual(contract.error_response.codes.length, requiredErrorCodes.length, "prover HTTP error code count");
  for (const code of requiredErrorCodes) {
    if (!contract.error_response.codes.includes(code)) {
      throw new Error(`prover HTTP error codes: missing ${code}`);
    }
  }
  assertEqual(contract.error_response.retryable_codes.length, 1, "prover HTTP retryable code count");
  assertEqual(contract.error_response.retryable_codes[0], "busy", "prover HTTP retryable busy code");
}

export function validateNoteReservationContract(contract: NoteReservationContract): void {
  assertEqual(contract.version, 3, "note reservation version");
  assertEqual(contract.fixture_migration.from_version, 1, "note reservation migration source version");
  assertEqual(contract.fixture_migration.to_version, 3, "note reservation migration target version");
  assertSchema(
    contract.fixture_migration.downstream_action.trim().length > 0,
    "note reservation migration action",
    "expected downstream migration guidance",
  );
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
  for (const [index, vector] of contract.operation_hash_test_vectors.entries()) {
    const canonicalRecipient = canonicalShieldedAddress(vector.recipient);
    const canonicalDenom = vector.denom.trim();
    assertSchema(
      canonicalDenom === vector.denom && validCosmosDenom(canonicalDenom),
      `note reservation amount hash vector ${index}`,
      "expected canonical Cosmos denom",
    );
    assertShieldedAmountString(vector.amount, `note reservation amount hash vector ${index}`);
    assertEqual(
      vector.recipient_hash,
      sha256Hex(canonicalRecipient),
      `note reservation recipient hash vector ${index}`,
    );
    assertEqual(
      vector.amount_hash,
      sha256Hex(`${canonicalDenom}:${BigInt(vector.amount).toString()}`),
      `note reservation amount hash vector ${index}`,
    );
  }
  for (const vector of contract.operation_hash_rejection_vectors) {
    if (vector.reject_hash === "recipient") {
      let rejected = false;
      try {
        canonicalShieldedAddress(vector.recipient);
      } catch {
        rejected = true;
      }
      assertSchema(rejected, `operation hash rejection ${vector.name}`, "expected an invalid shielded recipient");
      continue;
    }
    assertSchema(
      vector.denom !== vector.denom.trim() ||
        !validCosmosDenom(vector.denom) ||
        !/^(0|[1-9][0-9]*)$/.test(vector.amount) ||
        BigInt(vector.amount) > maxShieldedAmount,
      `operation hash rejection ${vector.name}`,
      "expected invalid denom or uint64 amount",
    );
  }

  assertTransitionSetEqual(
    contract.lease_transition_preconditions.token_required_for,
    expectedLeaseRequiredTransitions,
    "note reservation lease-required transitions",
  );
  assertTransitionSetEqual(
    contract.lease_transition_preconditions.recovery_without_token_after_expiry_for,
    expectedLeaseExpiryRecoveryTransitions,
    "note reservation expired-lease recovery transitions",
  );
  assertStringArrayEqual(contract.lease_transition_preconditions.fields, expectedLeaseFields, "note reservation lease fields");
  assertSchema(
    contract.lease_transition_preconditions.policy.trim().length > 0,
    "note reservation lease policy",
    "expected non-empty policy",
  );
  for (const vector of contract.transition_evidence_preconditions) {
    assertSchema(vector.name.trim().length > 0, "transition evidence name", "expected name");
    assertSchema(
      expectedAllowedTransitions.some((transition) => transitionKey(transition) === transitionKey(vector.transition)),
      `transition evidence ${vector.name}`,
      "expected an allowed transition",
    );
    for (const evidence of vector.required_evidence) {
      assertEqual(vector.positive[evidence], true, `transition evidence ${vector.name} positive ${evidence}`);
    }
    assertSchema(
      vector.required_evidence.some((evidence) => vector.negative[evidence] !== true),
      `transition evidence ${vector.name} negative`,
      "expected a missing required evidence value",
    );
  }
  assertStringArrayEqual(
    contract.manual_review_resolution.required_evidence,
    ["operator_approved", "operator_id", "operator_approval_reference"],
    "manual review evidence",
  );
  assertEqual(contract.manual_review_resolution.positive.operator_approved, true, "manual review positive approval");
  assertSchema(
    !String(contract.manual_review_resolution.negative.operator_id ?? "").trim(),
    "manual review negative approval",
    "expected missing operator identity",
  );
  assertEqual(contract.relay_handoff.status, "ProofReady", "relay handoff status");
  assertEqual(contract.relay_handoff.lease_must_remain, true, "relay handoff keeps lease");
  assertStringArrayEqual(contract.relay_handoff.record_requires, ["ProofReady", "lease_owner", "lease_token", "payload_hash_matches"], "relay handoff record requirements");
  assertEqual(contract.relay_handoff.proof_discard_after_handoff, "reject", "relay handoff proof discard policy");
  assertStringArrayEqual(contract.relay_handoff.write_once_evidence, ["payload_hash", "relay_handed_off", "relay_handed_off_at"], "relay handoff write-once evidence");
  assertEqual(contract.relay_handoff.positive.relay_handed_off, true, "relay handoff positive handoff");
  assertEqual(contract.relay_handoff.positive.lease_owner_present, true, "relay handoff positive owner");
  assertEqual(contract.relay_handoff.positive.lease_token_present, true, "relay handoff positive token");
  assertEqual(contract.relay_handoff.positive.payload_hash_matches, true, "relay handoff positive payload hash");
  assertEqual(contract.relay_handoff.negative.relay_handed_off, true, "relay handoff negative handoff");
  assertEqual(contract.relay_handoff.negative.lease_owner_present, false, "relay handoff negative owner");
  assertEqual(contract.relay_handoff.negative.lease_token_present, false, "relay handoff negative token");
  assertEqual(contract.relay_handoff.negative.payload_hash_matches, false, "relay handoff negative payload hash");
  assertEqual(contract.relay_handoff.negative_vectors.length, 3, "relay handoff negative vectors");
  assertEqual(contract.relay_handoff.negative_vectors[0]?.name, "payload_hash_mismatch", "relay handoff payload mismatch vector");
  assertEqual(contract.relay_handoff.negative_vectors[0]?.payload_hash_matches, false, "relay handoff payload mismatch rejection");
  assertEqual(contract.relay_handoff.negative_vectors[1]?.name, "mixed_reservation_status", "relay handoff mixed status vector");
  assertEqual(contract.relay_handoff.negative_vectors[1]?.all_reservations_proof_ready, false, "relay handoff mixed status rejection");
  assertEqual(contract.relay_handoff.negative_vectors[2]?.name, "partial_operation_reservation_set", "relay handoff partial operation vector");
  assertEqual(contract.relay_handoff.negative_vectors[2]?.operation_reservation_set_exact, false, "relay handoff partial operation rejection");
  assertEqual(contract.initial_state_preconditions.reservation_status, "Reserved", "initial reservation status");
  assertEqual(contract.initial_state_preconditions.operation_status, "Planned", "initial operation status");
  assertStringArrayEqual(contract.initial_state_preconditions.forbidden_reservation_evidence, ["lease", "payload_hash", "broadcast", "relay_handoff", "manual_review"], "initial reservation forbidden evidence");
  assertStringArrayEqual(contract.initial_state_preconditions.forbidden_operation_evidence, ["payload_hash", "tx_identity"], "initial operation forbidden evidence");
  assertEqual(contract.initial_state_preconditions.positive.reservation_clean, true, "initial reservation positive");
  assertEqual(contract.initial_state_preconditions.positive.operation_clean, true, "initial operation positive");
  assertEqual(contract.initial_state_preconditions.negative.reservation_clean, false, "initial reservation negative");
  assertEqual(contract.initial_state_preconditions.negative.operation_clean, false, "initial operation negative");
  assertEqual(contract.fail_closed_runtime_policy.nullifier_spent_evidence.spent_value, true, "runtime policy spent value");
  assertEqual(contract.fail_closed_runtime_policy.nullifier_spent_evidence.unspent_value, false, "runtime policy unspent value");
  assertEqual(
    contract.fail_closed_runtime_policy.nullifier_spent_evidence.other_values,
    "unknown_excluded_from_spending",
    "runtime policy unknown nullifier result",
  );
  assertEqual(
    contract.fail_closed_runtime_policy.relay_submission.chain_time_source,
    "latest_chain_block_time",
    "runtime policy relay chain time source",
  );
  assertEqual(contract.fail_closed_runtime_policy.relay_submission.chain_time_required, true, "runtime policy relay chain time required");
  assertEqual(
    contract.fail_closed_runtime_policy.relay_submission.recheck_immediately_before_broadcast,
    true,
    "runtime policy relay chain time recheck",
  );
  assertEqual(contract.fail_closed_runtime_policy.relay_submission.on_unavailable, "reject_submit", "runtime policy unavailable chain time");
  assertStringArrayEqual(
    contract.fail_closed_runtime_policy.heartbeat.coverage,
    ["proof_generation", "transaction_or_sign_doc_build", "proof_ready_transition"],
    "runtime policy heartbeat coverage",
  );
  assertEqual(contract.fail_closed_runtime_policy.heartbeat.await_in_flight_before_stop, true, "runtime policy heartbeat shutdown");
  assertEqual(
    contract.fail_closed_runtime_policy.broadcast_boundary.durable_attempt_before_external_call,
    true,
    "runtime policy durable broadcast attempt",
  );
  assertEqual(
    contract.fail_closed_runtime_policy.broadcast_boundary.retry_blocked_until_reconciled,
    true,
    "runtime policy broadcast retry gate",
  );
  const expectedWriteOnceFields = [
    "payload_hash",
    "submitted_tx_hash",
    "tx_bytes_hash",
    "sign_doc_hash",
    "expected_output_commitment",
    "expected_disclosure_digest",
    "expected_recipient_hash",
    "expected_amount",
    "expected_amount_hash",
    "expected_denom",
    "batch_item_index",
    "batch_item_index_known",
    "operation_success_evidence_required",
  ];
  assertStringArrayEqual(contract.evidence_immutability.write_once_fields, expectedWriteOnceFields, "write-once lifecycle evidence");
  assertStringArrayEqual(contract.evidence_immutability.monotonic_fields, ["broadcast_attempt_count"], "monotonic evidence");
  assertEqual(contract.evidence_immutability.negative.submitted_tx_hash, "", "write-once evidence negative vector");
  assertSchema(
    Array.isArray(contract.evidence_immutability.mutation_rejection_vectors),
    "write-once mutation rejection vectors",
    "expected an array",
  );
  assertStringArrayEqual(
    contract.evidence_immutability.mutation_rejection_vectors.map((vector) => vector.field),
    expectedWriteOnceFields,
    "write-once mutation rejection vector fields",
  );
  for (const vector of contract.evidence_immutability.mutation_rejection_vectors) {
    assertSchema(
      JSON.stringify(vector.original) !== JSON.stringify(vector.mutation),
      `write-once mutation rejection vector ${vector.field}`,
      "original and mutation must differ",
    );
  }
  assertStringArrayEqual(contract.spent_sibling_quarantine.match_fields, ["owner_key_id", "nullifier_lookup_key"], "spent sibling match fields");
  assertEqual(contract.spent_sibling_quarantine.target_status, "ConfirmedSpent", "spent sibling target status");
  assertEqual(contract.spent_sibling_quarantine.positive.confirmed_spent, contract.spent_sibling_quarantine.positive.matching_siblings, "spent sibling positive quarantine");
  assertSchema(
    contract.spent_sibling_quarantine.negative.confirmed_spent < contract.spent_sibling_quarantine.negative.matching_siblings,
    "spent sibling negative quarantine",
    "expected incomplete sibling quarantine",
  );
  assertStringArrayEqual(
    contract.success_evidence_required,
    expectedSuccessEvidenceRequired,
    "note reservation success evidence",
  );
  assertEqual(contract.operation_identity_evidence.required, "matching_persisted_tx_identity", "operation identity requirement");
  for (const vector of contract.operation_identity_evidence.vectors) {
    const allowedTxResultFields = new Set(["code", "txhash", "tx_bytes_hash", "sign_doc_hash"]);
    assertSchema(
      Object.keys(vector.tx_result).every((field) => allowedTxResultFields.has(field)),
      `operation identity vector ${vector.name}`,
      "tx_result contains an unsupported field",
    );
    assertSchema(
      Number.isSafeInteger(vector.tx_result.code) && Number(vector.tx_result.code) >= 0,
      `operation identity vector ${vector.name}`,
      "tx_result.code must be a non-negative safe integer",
    );
    const identity = (value: unknown, field: string): string => {
      if (value === undefined) return "";
      assertSchema(
        typeof value === "string" && value.trim().length > 0,
        `operation identity vector ${vector.name}`,
        `${field} must be a non-empty string`,
      );
      return (value as string).trim().toLowerCase().replace(/^0x/, "");
    };
    const storedTxHash = identity(vector.stored_tx_hash, "stored_tx_hash");
    const storedTxBytesHash = identity(vector.stored_tx_bytes_hash, "stored_tx_bytes_hash");
    const storedSignDocHash = identity(vector.stored_sign_doc_hash, "stored_sign_doc_hash");
    const actualTxHash = identity(vector.tx_result.txhash, "tx_result.txhash");
    const actualTxBytesHash = identity(vector.tx_result.tx_bytes_hash, "tx_result.tx_bytes_hash");
    const actualSignDocHash = identity(vector.tx_result.sign_doc_hash, "tx_result.sign_doc_hash");
    assertSchema(
      Boolean(storedTxHash || storedTxBytesHash),
      `operation identity vector ${vector.name}`,
      "stored_tx_hash or stored_tx_bytes_hash is required",
    );
    const sameFieldMismatch =
      (Boolean(actualTxHash) && actualTxHash !== storedTxHash) ||
      (Boolean(actualTxBytesHash) && actualTxBytesHash !== storedTxBytesHash) ||
      (Boolean(storedSignDocHash) && Boolean(actualSignDocHash) && actualSignDocHash !== storedSignDocHash);
    const matched = vector.tx_result.code === 0 && !sameFieldMismatch && (
      (Boolean(actualTxHash) && actualTxHash === storedTxHash) ||
      (Boolean(storedTxBytesHash) && actualTxBytesHash === storedTxBytesHash)
    );
    assertEqual(
      vector.operation_status,
      matched ? "Succeeded" : "ConflictSpent",
      `operation identity vector ${vector.name}`,
    );
  }
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

const executedPath = process.argv[1] ? resolve(process.argv[1]) : "";
if (executedPath === fileURLToPath(import.meta.url)) {
  main();
}
