#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

INPUT="${INPUT:-examples/reference-payroll/payroll-demo.json}"
OUT_DIR="${OUT_DIR:-tmp/reference-payroll-demo}"
PAYROLL_BIN="${PAYROLL_BIN:-}"
PAYROLLD_BIN="${PAYROLLD_BIN:-}"

mkdir -p "$OUT_DIR"

run_payroll() {
  if [[ -n "$PAYROLL_BIN" ]]; then
    "$PAYROLL_BIN" "$@"
  else
    go run ./cmd/clairveil-payroll "$@"
  fi
}

run_payrolld() {
  if [[ -n "$PAYROLLD_BIN" ]]; then
    "$PAYROLLD_BIN" "$@"
  else
    go run ./cmd/clairveil-payrolld "$@"
  fi
}

VALIDATION="$OUT_DIR/validation.json"
PREPARE="$OUT_DIR/note-preparation.json"
PLAN="$OUT_DIR/plan.json"
CONFIRMED_PLAN="$OUT_DIR/confirmed-plan.json"
STATE="$OUT_DIR/reservation-state.json"
STATUS_BEFORE="$OUT_DIR/status-before-daemon.json"
DAEMON_REPORT="$OUT_DIR/payrolld-report.json"
STATUS_AFTER="$OUT_DIR/status-after-daemon.json"
FINAL_REPORT="$OUT_DIR/final-report.json"

run_payroll validate -input "$INPUT" -out "$VALIDATION"
run_payroll prepare-notes -input "$INPUT" -out "$PREPARE"
run_payroll plan -input "$INPUT" -out "$PLAN"
run_payroll run -plan "$PLAN" -state "$STATE" -out "$CONFIRMED_PLAN"
run_payroll status -state "$STATE" -out "$STATUS_BEFORE"
run_payrolld -state "$STATE" -once -out "$DAEMON_REPORT"
run_payroll status -state "$STATE" -out "$STATUS_AFTER"
run_payroll export-report -plan "$PLAN" -state "$STATE" -out "$FINAL_REPORT"

cat <<EOF
Reference payroll demo complete.

Input:             $INPUT
Output directory:  $OUT_DIR
Validation:        $VALIDATION
Preparation:       $PREPARE
Plan:              $PLAN
Confirmed plan:    $CONFIRMED_PLAN
Daemon report:     $DAEMON_REPORT
Final status:      $STATUS_AFTER
Final report:      $FINAL_REPORT
EOF
