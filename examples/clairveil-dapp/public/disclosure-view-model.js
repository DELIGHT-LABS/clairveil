export function disclosureViewModel(report) {
  if (report?.verification?.verified !== true) {
    return { verified: false, summary: null, payload: null };
  }
  return {
    verified: true,
    summary: report?.summary || {},
    payload: report?.payload || {}
  };
}
