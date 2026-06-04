# Audit Disclosure Key 예제

이 예제는 JS/TS wallet 또는 운영 도구가 `app_state.privacy.audit_master_pubkey`에 들어가는 Clairveil audit disclosure public key 형식을 만드는 방법을 보여줍니다.

## 보여주는 내용

- 명시적인 private scalar에서 deterministic keypair를 만듭니다.
- seed bytes에서 deterministic keypair를 만듭니다.
- Clairveil privacy root signer material에서 disclosure keypair를 파생합니다.
- 새로운 random keypair를 만듭니다.
- public key를 genesis에 넣을 수 있는 base64 값으로 인코딩합니다.

public key는 32-byte compressed BN254 twisted-Edwards point입니다. genesis JSON은 이 bytes를 base64로 저장합니다. private key는 hex scalar이며 반드시 secret으로 보관해야 합니다.

## 실행

이 디렉터리에서 실행합니다.

```bash
npm test
npm run demo
```

repository root에서 실행합니다.

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/audit-disclosure-keys run demo
```

이 예제는 Node built-in만 사용하며 npm dependency가 없습니다.

## 사용 메모

`keypairFromPrivateKeyHex` 또는 `keypairFromSeedHex`는 reproducible test vector가 필요할 때만 사용하십시오. 같은 입력은 항상 같은 private key와 public key를 만듭니다.

새 standalone audit disclosure keypair가 필요하면 `randomKeypair`를 사용하십시오. private scalar는 안전하게 보관하고 `publicKeyGenesisBase64` 값을 `app_state.privacy.audit_master_pubkey`에 넣습니다.

CLI-style identity derivation flow와 맞춰야 한다면 `keypairFromPrivacyRootSigner`를 사용하십시오. signer adapter는 address, transparent public key bytes, `signPrivacyRoot(messageBytes)`를 제공해야 합니다.
