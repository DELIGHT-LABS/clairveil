function keplrReservationIDs(options = {}) {
  const reservation = options.reservation
    ?? options.reservationBatch
    ?? options.reservation_batch
    ?? null;
  const ids = reservation?.reservation_ids ?? reservation?.reservationIds;
  return Array.isArray(ids) ? ids.filter(Boolean) : [];
}

export function keplrDirectSignOptions(options = {}) {
  return Object.freeze({
    // ProofReady stores a hash of TxBody + AuthInfo. Keplr may rewrite a
    // zero-fee AuthInfo unless preferNoSetFee is set, invalidating that hash
    // after the wallet signs. Non-reserved deposits can still use Keplr's fee
    // editor because they have no note reservation artifact to preserve.
    preferNoSetFee: keplrReservationIDs(options).length > 0,
    preferNoSetMemo: true
  });
}
