# Clairveil Merkle Restore SOP

This document is the minimum operating procedure operators should follow after snapshot, restore, or migration of Clairveil privacy Merkle state.

Korean version: [clairveil-merkle-restore-sop-kr.md](clairveil-merkle-restore-sop-kr.md)

## Restore Validation Checklist

1. Restore `Leaf/*`, `MerkleNode/*`, `CommitmentIndex/*`, `HistoricalRoot/*`, cached root, and `leaf_count` from a consistent snapshot at the same height.
2. Do not treat a matching cached root alone as a successful restore if some `Leaf/*` or lower `MerkleNode/*` entries are missing.
3. After restore, query `tree_state` and confirm `leaf_count`, `max_leaves`, `remaining_leaves`, and `root` match expectations.
4. Choose at least two sample commitments: one old commitment and one recently appended commitment.
5. For each sample, confirm the `merkle_path` query returns `path` and `path_helper` without error.
6. Do not only compare `merkle_path.root` with `tree_state.root`.
7. Recompute the root off-chain from the sample commitment bytes, returned `path`, and returned `path_helper`.
8. The off-chain computed root must match `tree_state.root`.
9. If a large tree above `MaxMerkleRebuildLeaves` has no cached root, the keeper intentionally does not auto-recover it.
10. In that case, do not retry the query as the normal response. Use an offline rebuild tool, state restore revalidation, or a separate migration plan.

## Operations Principle

`TreeState` does not scan every lower `MerkleNode/*` on every query. This is an intentional separation of responsibility to avoid high query cost. `TreeState` provides quick state inspection, while lower-node integrity is covered by sampled `MerklePath` recomputation and required-node checks in append/write paths.

Therefore, restore completion must mean more than `TreeState` success alone. It must include successful sampled path recomputation.
