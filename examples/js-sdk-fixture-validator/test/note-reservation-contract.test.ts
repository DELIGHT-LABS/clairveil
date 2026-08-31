import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  canonicalCompressedPoint,
  type JsonSchemaDocument,
  type NoteReservationContract,
  validateJSONSchema,
  validateNoteReservationContract,
} from "../src/index.ts";

const fixturePath = join(
  dirname(fileURLToPath(import.meta.url)),
  "../../../x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json",
);
const walletSchemaPath = join(
  dirname(fileURLToPath(import.meta.url)),
  "../../../docs/schemas/clairveil-js-wallet-contract.schema.json",
);

function readContract(): NoteReservationContract {
  return JSON.parse(readFileSync(fixturePath, "utf8")) as NoteReservationContract;
}

test("wallet coin strings accept the same mixed-case denoms as denom fields", () => {
  const schema = JSON.parse(readFileSync(walletSchemaPath, "utf8")) as JsonSchemaDocument;
  const definitions = (schema as any).$defs;
  const coinString = definitions.coinString as JsonSchemaDocument;
  const referenceWithdrawAmount = definitions.sendCapableReferenceFlow
    .properties.withdraw.properties.amount as JsonSchemaDocument;

  assert.doesNotThrow(() =>
    validateJSONSchema("1UCLAIR", coinString, "coinString", schema),
  );
  assert.doesNotThrow(() =>
    validateJSONSchema("1UCLAIR", referenceWithdrawAmount, "withdraw.amount", schema),
  );
});

test("JSON Schema oneOf requires exactly one matching branch", () => {
  const schema: JsonSchemaDocument = {
    oneOf: [
      { type: "string", pattern: "^clair" },
      { type: "string", minLength: 3 },
    ],
  };

  assert.doesNotThrow(() => validateJSONSchema("abc", schema, "oneOf", schema));
  assert.throws(
    () => validateJSONSchema("clair", schema, "oneOf", schema),
    /expected exactly one oneOf schema to match, got 2/,
  );
  assert.throws(
    () => validateJSONSchema(7, schema, "oneOf", schema),
    /expected exactly one oneOf schema to match, got 0/,
  );
});

test("compressed points reject non-canonical y coordinates", () => {
  const fieldModulus = 21888242871839275222246405745257275088548364400416034343698204186575808495617n;
  const bytes = Uint8Array.from(
    Buffer.from(fieldModulus.toString(16).padStart(64, "0"), "hex"),
  ).reverse();

  assert.throws(() => canonicalCompressedPoint(bytes), /non-canonical y coordinate/);
});

test("relay handoff requires both lease owner and token evidence", () => {
  const valid = readContract();
  validateNoteReservationContract(valid);

  const missingPositiveToken = structuredClone(valid);
  missingPositiveToken.relay_handoff.positive.lease_token_present = false;
  assert.throws(() => validateNoteReservationContract(missingPositiveToken), /relay handoff positive token/);

  const presentNegativeToken = structuredClone(valid);
  presentNegativeToken.relay_handoff.negative.lease_token_present = true;
  assert.throws(() => validateNoteReservationContract(presentNegativeToken), /relay handoff negative token/);
});

test("relay handoff binds every reservation to the exact payload hash", () => {
  const mismatchedPayload = readContract();
  mismatchedPayload.relay_handoff.positive.payload_hash_matches = false;

  assert.throws(() => validateNoteReservationContract(mismatchedPayload), /relay handoff positive payload hash/);

  const mixedStatus = readContract();
  mixedStatus.relay_handoff.negative_vectors[1].all_reservations_proof_ready = true;

  assert.throws(() => validateNoteReservationContract(mixedStatus), /relay handoff mixed status rejection/);

  const partialOperation = readContract();
  partialOperation.relay_handoff.negative_vectors[2].operation_reservation_set_exact = true;

  assert.throws(() => validateNoteReservationContract(partialOperation), /relay handoff partial operation rejection/);
});

test("operation identity evidence cannot use a different field or sign doc alone", () => {
  const crossField = readContract();
  const crossFieldVector = crossField.operation_identity_evidence.vectors.find(
    (vector: { name: string }) => vector.name === "tx hash does not match tx bytes field",
  );
  assert.ok(crossFieldVector);
  crossFieldVector.operation_status = "Succeeded";
  assert.throws(() => validateNoteReservationContract(crossField), /tx hash does not match tx bytes field/);

  const signDocOnly = readContract();
  const signDocVector = signDocOnly.operation_identity_evidence.vectors.find(
    (vector: { name: string }) => vector.name === "sign doc alone is not chain identity",
  );
  assert.ok(signDocVector);
  signDocVector.operation_status = "Succeeded";
  assert.throws(() => validateNoteReservationContract(signDocOnly), /sign doc alone is not chain identity/);

  const malformedIdentity = readContract();
  const successVector = malformedIdentity.operation_identity_evidence.vectors.find(
    (vector: { name: string }) => vector.name === "matching identity succeeds",
  );
  assert.ok(successVector);
  (successVector.tx_result as any).txhash = ["EXPECTED-TX"];
  assert.throws(() => validateNoteReservationContract(malformedIdentity), /tx_result\.txhash must be a non-empty string/);

  const missingStoredIdentity = readContract();
  const bareVector = missingStoredIdentity.operation_identity_evidence.vectors[0];
  delete bareVector.stored_tx_hash;
  assert.throws(() => validateNoteReservationContract(missingStoredIdentity), /stored_tx_hash or stored_tx_bytes_hash is required/);

  const mismatchedSignDoc = readContract();
  const mismatchedSignDocVector = mismatchedSignDoc.operation_identity_evidence.vectors.find(
    (vector: { name: string }) => vector.name === "sign doc mismatch conflicts with matching tx identity",
  );
  assert.ok(mismatchedSignDocVector);
  mismatchedSignDocVector.operation_status = "Succeeded";
  assert.throws(() => validateNoteReservationContract(mismatchedSignDoc), /sign doc mismatch conflicts/);

  const missingActualIdentity = readContract();
  const bytesVector = missingActualIdentity.operation_identity_evidence.vectors.find(
    (vector: { name: string }) => vector.name === "matching tx bytes identity succeeds",
  );
  assert.ok(bytesVector);
  delete bytesVector.tx_result.tx_bytes_hash;
  assert.throws(() => validateNoteReservationContract(missingActualIdentity), /matching tx bytes identity succeeds/);
});

test("operation hashes require canonical shielded recipients and non-empty denoms", () => {
  const invalidRecipient = readContract();
  invalidRecipient.operation_hash_test_vectors[0].recipient = "not-a-shielded-address";
  invalidRecipient.operation_hash_test_vectors[0].recipient_hash = "00".repeat(32);
  assert.throws(() => validateNoteReservationContract(invalidRecipient), /bech32|shielded recipient/i);

  const emptyDenom = readContract();
  emptyDenom.operation_hash_test_vectors[0].denom = "   ";
  assert.throws(() => validateNoteReservationContract(emptyDenom), /canonical Cosmos denom/);

  const offCurveRecipient = readContract();
  const offCurveVector = offCurveRecipient.operation_hash_rejection_vectors.find(
    (vector: { name: string }) => vector.name === "shielded recipient point outside curve",
  );
  assert.ok(offCurveVector);
  offCurveRecipient.operation_hash_test_vectors[0].recipient = offCurveVector.recipient;
  assert.throws(
    () => validateNoteReservationContract(offCurveRecipient),
    /non-canonical y coordinate|point is not on the Clairveil curve/,
  );

});

test("operation amount hashes require canonical uint64 amounts", () => {
  for (const amount of ["-1", "01", "18446744073709551616"]) {
    const invalidAmount = readContract();
    invalidAmount.operation_hash_test_vectors[0].amount = amount;
    assert.throws(
      () => validateNoteReservationContract(invalidAmount),
      /canonical non-negative decimal string|expected <= 18446744073709551615/,
    );
  }
});

test("normal creation starts with clean lifecycle state", () => {
  const forgedReservation = readContract();
  forgedReservation.initial_state_preconditions.positive.reservation_clean = false;

  assert.throws(() => validateNoteReservationContract(forgedReservation), /initial reservation positive/);

  const forgedOperation = readContract();
  forgedOperation.initial_state_preconditions.negative.operation_clean = true;

  assert.throws(() => validateNoteReservationContract(forgedOperation), /initial operation negative/);
});

test("operation success predicate fields are all write-once", () => {
  const missingField = readContract();
  missingField.evidence_immutability.write_once_fields.pop();
  assert.throws(() => validateNoteReservationContract(missingField), /write-once lifecycle evidence/);

  const missingVector = readContract();
  missingVector.evidence_immutability.mutation_rejection_vectors =
    missingVector.evidence_immutability.mutation_rejection_vectors.slice(0, -1);
  assert.throws(() => validateNoteReservationContract(missingVector), /mutation rejection vector fields/);

  const ineffectiveMutation = readContract();
  ineffectiveMutation.evidence_immutability.mutation_rejection_vectors[4].mutation = "output-a";
  assert.throws(() => validateNoteReservationContract(ineffectiveMutation), /expected_output_commitment/);
});
