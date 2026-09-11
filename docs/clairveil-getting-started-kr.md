# Clairveil 시작 가이드

> English version: [clairveil-getting-started.md](clairveil-getting-started.md)

Clairveil은 검토된 고정 privacy verifier artifact를 사용합니다. Node 초기화 중에 trusted setup material, replay bundle, audit private key를 생성하지 않습니다.

## 전제조건

Go `1.25.12`, Git, Make, Bash와 네 개의 필수 verifier artifact가 들어 있는 검토된 directory가 필요합니다. Artifact directory는 실행 중인 node에만 제공하며 genesis에 복사하지 않습니다.

검토된 작은 V4 구성 파일을 준비합니다. JSON 필드는 `chain_id`, base64 `network_nonce32`, `initial_height`, `initial_audit_key`, `circuit_set_identity`입니다. `initial_audit_key`에는 public key와 proof of possession만 들어갑니다(`epoch`, base64 `key_id`, `suite`, base64 `public_key`, base64 `pop`). Circuit identity는 local artifact manifest와 정확히 일치해야 합니다.

## 초기화와 시작

```bash
clairveild init node-1 \
  --chain-id reviewed-chain-1 \
  --audit-config /absolute/path/to/audit-config.json

clairveild start \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/reviewed-artifacts
```

`init`은 chain ID와 initial height가 구성 파일과 일치하는지 확인하고 initial audit key, network nonce, circuit identity, canonical asset registry가 든 fresh V4 privacy genesis를 씁니다. `start`는 같은 구성을 local verifier artifact와 대조합니다. Runtime archive, source bundle, offline secret 입력은 없습니다.

`clairveild export`에도 같은 두 flag를 사용하세요. Export에는 일반 privacy state와 작은 V4 metadata가 함께 들어가며, 일반 Cosmos 초기화 경로로 다시 import할 수 있습니다. Zero-height export와 partial-module export는 의도적으로 지원하지 않습니다.

## Audit provenance

`clairveil-auditor`에 닫힌 block 범위, 일반 Cosmos chain ID, owner-only retained audit-key JSON file, reviewed verifier artifact directory를 전달합니다. Tool은 작은 public configuration과 key history를 query하고 atomic original-tx/result cache에서 재개한 뒤, 수집한 message를 검증·복호화하며 collection completeness와 lineage completeness를 분리해 보고합니다. 자세한 내용은 [CLI reference](clairveil-cli-reference-kr.md#audit-provenance-수집과-검증)를 참고하세요.

## 검증

```bash
go test ./app ./cmd/clairveild/cmd ./x/privacy/keeper ./x/privacy/client/cli
make build
```

이 development-oriented workflow를 production trusted setup으로 사용하지 마세요. Operator key를 보호하고 disposable development home을 지우기 전에 내용을 검토하세요.

## 8. Legacy BatchJoinSplit16x32 reference

보존된 restartable multi-message batch/payroll command와 `/v1/proofs/batch-transfer` endpoint는 compatibility/regression 도구이며 현재 one-proof audit-field workflow가 아닙니다. 그 결과를 V2 audit provenance나 capacity evidence로 사용하면 안 됩니다. Active `MsgBatchTransfer` contract는 [BatchJoinSplit16x32 reference](clairveil-batch-joinsplit-16x32-kr.md)에 설명되어 있습니다.
