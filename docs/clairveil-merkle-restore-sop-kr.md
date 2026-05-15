# Clairveil Merkle Restore SOP

이 문서는 Clairveil privacy Merkle state를 snapshot, restore, migration한 뒤 운영자가 최소한으로 확인해야 하는 절차입니다.

## Restore 검증 체크리스트

1. `Leaf/*`, `MerkleNode/*`, `CommitmentIndex/*`, `HistoricalRoot/*`, cached root, `leaf_count`를 같은 height의 일관된 snapshot에서 복구합니다.
2. `Leaf/*`나 하위 `MerkleNode/*` 일부가 빠진 상태에서 cached root 하나만 맞는 것을 정상 복구로 보지 않습니다.
3. 복구 후 `tree_state` query로 `leaf_count`, `max_leaves`, `remaining_leaves`, `root`가 예상값인지 확인합니다.
4. 오래된 commitment 1개와 최근 append된 commitment 1개를 최소 샘플로 고릅니다.
5. 각 샘플에 대해 `merkle_path` query가 error 없이 path와 `path_helper`를 반환하는지 확인합니다.
6. `merkle_path.root` 값만 `tree_state.root`와 비교하지 않습니다.
7. 샘플 commitment bytes, returned `path`, returned `path_helper`로 root를 오프체인에서 다시 계산합니다.
8. 오프체인에서 계산한 root가 `tree_state.root`와 일치해야 합니다.
9. `leaf_count`가 `MaxMerkleRebuildLeaves`를 넘는 큰 tree에서 cached root가 없으면 keeper가 자동 복구하지 않습니다.
10. 이 경우 query 재시도가 아니라 offline rebuild tool, state restore 재검증, 또는 별도 migration plan으로 대응합니다.

## 운영 원칙

`TreeState`는 매 query마다 전체 lower `MerkleNode/*`를 스캔하지 않습니다. 이것은 비용을 피하기 위한 의도적인 책임 분리입니다. 빠른 상태 확인은 `TreeState`가 담당하고, lower node 무결성은 실제 `MerklePath` 샘플 재계산과 append/write 경로의 required-node check가 담당합니다.

따라서 restore 완료 선언은 `TreeState` 단독 성공이 아니라, 샘플 path 재계산 성공까지 포함해야 합니다.
