# Suite 2 독립 계약

이 패키지는 감사용 canonical DTO와 codec의 단일 소유자다. 상위 `crypto`, `types`, SDK를 import하지 않는다. 이 문서는 P0 독립 계약과 P1 native 암호 API의 경계를 설명한다. 체인 출처 인증은 상위 계층의 책임이다.

## 형식과 의존 경계

- `F32`는 BN254 Fr의 정확한 32-byte big-endian 값이며 `0 <= x < 21888242871839275222246405745257275088548364400416034343698204186575808495617`이다. 비정규 값을 reduction하여 수용하지 않는다.
- 감사 version 2, suite 2, plaintext schema 1, context version 2를 사용한다. kind는 deposit=1, withdraw=2, transfer2x2=3, batch16x32=4뿐이다.
- 각 kind의 input/output capacity는 0/1, 1/0, 2/2, 16/32다. Batch의 활성 count는 각각 1..16, 1..32이고 나머지는 capacity와 정확히 같다.
- Envelope는 `Header32 || Nonce16 || E.X32 || E.Y32 || cipher[n] || tag32`다. Header는 `SHA256("clairveil.audit.envelope.v1")[0:16] || u16be(2) || u16be(2) || kind || inputs || outputs || zero[9]`이다. plaintext field 수와 frame byte 수는 각각 6/336, 2/208, 13/560, 177/5808이다.
- `EnvelopeFrame`/`ParseEnvelopeFrame`은 길이, header, count, 예약 바이트, Fr 정규성과 E의 on-curve·prime subgroup·nonidentity를 검사한다. `Point64`는 검증된 점만 보유하고 `AuditKey`는 정규 Point64로 KeyID를 재계산한다.
- Context는 nonce-free Kind와 PI0..20을 소유한다. T는 `[2,2,kind] || PI0..20 || nonce128`의 25 fields다. nonce는 실제 frame에서 얻는다. Context 구조 검사는 registry, 메시지, proof 또는 체인 출처 인증을 대신하지 않는다.
- `EncodeAuxFrame`은 독립 구조 encoder다. 중첩 recovery/disclosure envelope header·plaintext metadata 검증은 완료하지 않았으며, 중첩 payload의 완전한 message 의미 검증은 P4 adapter와 결합해야 한다. Aux는 `u16be(1)||kind||activeOutputCount` 뒤에 output 순서대로 recovery ciphertext, view tag, policy, mode, user digest, user target, user payload, self digest, self payload를 인코딩한다. 가변 bytes는 `u32be(length)||bytes`, policy는 u32be, mode는 u8, digest는 F32다. Audit envelope, commitment, proof, creator, fee는 Aux에 넣지 않는다. Recipient-encrypted target은 정규 compressed point와 subgroup을 검증하며 mode/policy/digest 일관성을 검사한다. Fixed payload 길이는 deposit recovery 398, transfer/batch recovery 430, view tag 2, public disclosure 392, encrypted disclosure 472 bytes다. 실제 generated `OutputEffect` adapter는 P4 소유다.

공개 domain helper는 다음 정확한 식을 제공한다. LP는 u32be 길이 prefix다.

- `NetworkDigest`: SHA256(`clairveil.audit.network.v1 || LP(chain_id) || genesis_nonce32 || LP(set_id)`), chain ID는 비어 있지 않은 printable ASCII다.
- `PublicTargetDigest`: SHA256(`clairveil.audit.public-target.v1 || kind || LP(address_bytes)`), deposit/withdraw에만 사용하며 canonical 주소 변환은 message adapter가 맡는다.
- `AuditContext.ContextHash`: SHA256(`clairveil.audit.context.v2 || F32(T[0]) ... F32(T[24])`).
- `DigestFields`는 SHA256 digest를 손실 없이 uint128 Hi/Lo로 나눈다. KeyID domain은 정의하되 검증되지 않은 raw point의 KeyID 생성 API는 공개하지 않는다.

`AuditPlain`의 일반 fmt/Text/JSON 출력은 차단한다. 명시적인 `Fields()`는 방어 복사를 반환하는 비밀값 export 경계이며 로깅에 사용하지 않는다. Go heap/register/compiler copy의 완전한 zeroization은 보장하지 않는다.

## 공개 schema와 manifest

Set ID는 `privacy-note-v1-audit-field-v1`이다. `zk.AuditFieldCircuitIDs`는 다음 네 descriptor만 반환한다.

| Circuit ID | PI23 schema SHA256 |
|---|---|
| `deposit-audit-field-v1` | `5582ac050f0aecc4a40d02acfc948deb94932443e9857167b8a15602c5f9df7e` |
| `spend-audit-field-v1` | `2e9cd75633922450f0aab068b2af26b94b2b3d71cc75426303fcf88554e5b8fa` |
| `joinsplit-2x2-audit-field-v1` | `79c7ff4a0ba411ccfaee0376291f9c1dfe65b4e38acf6ea1384749eefb07e333` |
| `batch-joinsplit-16x32-audit-field-v1` | `93668901c968802861b13658290c2d703f6837de8ae7af7ef0bf65c906bbfd84` |

PI 순서와 encoding 이름의 소유자는 `PublicInputSchema()`다. `zk.PublicInputSchemaSHA256`는 기존 `clairveil.public-input-schema.v1` encoder를 사용한다. Schema hash preimage는 domain 뒤에 LP(circuit ID), u32be(field count), 각 field의 u32be(1-based index), LP(name), LP(encoding) 순이다.

`zk.AuditFieldArtifactDescriptors()`는 위 circuit 순서로 각각 R1CS, proving key, verifying key의 총 12개 descriptor를 반환한다. 파일 이름은 `privacy_` 뒤에 circuit ID의 hyphen을 underscore로 치환한 값과 `_r1cs.bin`, `_pk.bin`, `_vk.bin`을 붙인다. Checksum 변수 이름은 `CLAIRVEIL_` 뒤에 같은 stem과 suffix를 대문자로 치환하고 `_SHA256`을 붙인다.

`ValidateAuditFieldArtifactManifest`는 기존 manifest 구조의 version/curve/set, 정확한 descriptor 순서와 수, canonical lowercase SHA256, VK/schema identity 일치를 검사한다. 파일 읽기와 setup 신뢰 검증은 수행하지 않는다. 실제 artifact hash는 canonical serializer로 만든 bytes에서 구해야 한다. 개발 키나 가짜 hash를 제공하지 않는다. 기본 registry는 기존 set을 유지하며 새 manifest를 수용하지 않는다. 새 회로·handler·state·SDK를 완성한 release에서 활성화한다.

## 고정 공개 상수

`params_literals.go`는 80개 round constants와 9개 tag 객체(18 F32)를 BE32 literal로 저장한다. Accessor는 배열 복사본을 반환한다. Runtime에 상수 생성기나 임의 domain/AD 암호 API를 제공하지 않는다.

Poseidon2는 t=3, rate=1, capacity=2, x^5, RF=8, RP=56이다. External matrix는 대각2/비대각1, internal matrix는 전체1에 대각 `(1,1,2)`를 더한 행렬이다. Round 0..3 및 60..63은 각각 상수3개, 나머지 round는 각각1개다. Public seed `Poseidon2-BN254[t=3,rF=8,rP=56,d=5]`의 LegacyKeccak256 hash를 먼저 구하고, 다음 hash부터 순차적으로 big-endian mod p하여 80개를 얻는다. pinned gnark 0.14.0 / gnark-crypto 0.19.2와 대응하며 공식 Grain 생성 상수라는 주장은 하지 않는다.

80개 BE32 연결 SHA256: `798b5a6caa6424ba671230365e197d380a329f84eb153d750ddc03aaeebdb92d`.

Static tag preimage는 `clairveil.audit.static-tag.v1 || u32be(word_count) || words_u32be || LP(domain)`이다. Absorb(n)은 `0x80000000|n`, Squeeze(n)은 n이다. 인접 동일 operation은 tag 생성에서만 병합한다. SHAKE256 stream에서 32-byte BE 후보 `<p`인 값 두 개를 순서대로 채택하며 reduction하지 않는다.

| Tag 순서 | Domain | 병합한 IO |
|---|---|---|
| 0 | `clairveil.audit.field.kdf.v1` | A29, S2 |
| 1..4 | `clairveil.audit.field.ae.v1` | A30, n회 (S1,A1), S1; n=6,2,13,177 |
| 5..8 | `clairveil.audit.field.root.v1` | A34/A30/A41/A205, S2 |

18개 BE32 연결 SHA256: `7c7361d62619ba864102ab0fce835e3ff5ae264cc6e4e347cf1bf9f68b38d47c`.

실제 H2는 Squeeze(1)을 두 번 호출하며 두 번째 호출 전에 permutation이 필요하다. 위 S2는 tag preimage의 병합 표현이다. Initialization tag 2 Fr와 final authentication tag 1 Fr를 구분한다. Sponge/KDF/AE는 fixed native 산술로 실행한다. 공개 EncryptAudit는 fresh scalar/nonce를 샘플하며 DecryptAudit는 context/key/root/own-key/tag를 모두 확인한 뒤에만 plaintext를 반환한다.

상수 재생성과 해시 대조는 `go test ./x/privacy/crypto/auditfield -run T13`에서 실행한다. 제품 build는 외부 인계 경로나 네트워크에 의존하지 않는다. Fr/q 산술 provenance와 생성물 license는 `../internal/ctbn254`에 보존한다. T01/T19 전체 및 전문 암호검토 T20은 후속 수락 항목이다.
