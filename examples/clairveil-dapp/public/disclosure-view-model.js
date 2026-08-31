export function disclosureViewModel(report) {
  if (report?.verification?.verified !== true) {
    return {
      verified: false,
      plane: "",
      policy: "",
      outputIndex: null,
      commitmentHex: "",
      digestHex: "",
      summary: null,
      payload: null
    };
  }
  const summary = report?.summary || {};
  const payload = report?.payload || {};
  return {
    verified: true,
    plane: report?.plane || summary.plane || payload.plane || "",
    policy: report?.policy || summary.policy || payload.policy || "",
    outputIndex: report?.output_index ?? payload.output_index ?? null,
    commitmentHex: report?.commitment_hex || payload.commitment_hex || "",
    digestHex: report?.digest_hex || payload.disclosure_digest_hex || "",
    summary,
    payload
  };
}
