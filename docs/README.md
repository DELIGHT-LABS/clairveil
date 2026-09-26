# Clairveil Documentation

> Korean version: [README-kr.md](README-kr.md)

Read documentation from the same tag or commit as the code. Start with the [repository overview](../README.md), then choose the guide for your task below.

## Guides And Contracts

| Reader task | English | 한국어 |
| --- | --- | --- |
| Set up and run privacy/batch flows | [Getting started](clairveil-getting-started.md) | [시작 가이드](clairveil-getting-started-kr.md) |
| Understand components and data flow | [Architecture](clairveil-architecture.md) | [아키텍처](clairveil-architecture-kr.md) |
| Understand each circuit’s proof boundary | [Circuits](clairveil-circuits.md) | [회로](clairveil-circuits-kr.md) |
| Consult historical NoteV1 and 16x32 encodings | [Legacy protocol contract](clairveil-batch-joinsplit-16x32.md) | [과거 프로토콜 계약](clairveil-batch-joinsplit-16x32-kr.md) |
| Look up commands, flags and output | [CLI reference](clairveil-cli-reference.md) | [CLI 참조](clairveil-cli-reference-kr.md) |
| Wire a downstream Cosmos app | [Cosmos integration](clairveil-downstream-cosmos-integration-guide.md) | [Cosmos 통합](clairveil-downstream-cosmos-integration-guide-kr.md) |
| Implement an SDK or wallet | [JS/TS SDK guide](clairveil-js-sdk-handoff.md) | [JS/TS SDK 가이드](clairveil-js-sdk-handoff-kr.md) |
| Integrate proof routes, including deposit | [Proverd HTTP API](clairveil-proverd-http-api.md) | [Proverd HTTP API](clairveil-proverd-http-api-kr.md) |
| Operate nodes/provers and restore state | [Operations](clairveil-operations-guide.md) | [운영](clairveil-operations-guide-kr.md) |
| Select tests and release evidence | [Testing](clairveil-testing-guide.md) | [테스트](clairveil-testing-guide-kr.md) |
| Review trust boundaries and residual risks | [Threat model](clairveil-threat-model.md) | [위협 모델](clairveil-threat-model-kr.md) |

## Example And Contributor Documentation

- [Reference payroll](../examples/reference-payroll/README.md): one-proof/legacy boundaries, durable reservation/retry/reconciliation contracts, and the demo.
- [Contributing](../CONTRIBUTING.md): change checklist, documentation policy, and release versioning/packaging.
- [Schemas](schemas/README.md): machine-readable wallet and prover contracts and validation commands. Canonical fixtures live in [Go SDK conformance testdata](../x/privacy/client/sdk/conformance/testdata).

## Authority And Maintenance

Compiled proto/message/query definitions and keeper validation define executable behavior. The protocol contract, schemas, and conformance fixtures define frozen cross-language encodings. CLI help and the CLI reference define command surfaces; guides explain use and operations.

Keep one source for each subject: local workflows in getting started; proof transport and deposit semantics in the HTTP API; restore and remote-prover deployment in operations; example contracts beside their implementation; contributor/release rules in `CONTRIBUTING.md`. Update English/Korean pairs together. Add a standalone document only for a distinct reader task that an existing guide cannot cover clearly.

`docs/` holds current shared knowledge. Plans, dated research, completion records and drafts belong in ignored `tmpdocs/`; generated runtime data belongs in `tmp/`, which must not contain Markdown. Tracked documentation must not link to ignored archives.

Release membership is defined by `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt`. Run `make docs-check` after documentation changes.
