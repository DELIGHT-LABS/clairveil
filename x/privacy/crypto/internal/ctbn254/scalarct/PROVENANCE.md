# Fiat-Crypto 생성물 provenance

- 생성기: Fiat-Crypto v0.1.6 `word-by-word-montgomery`, Go, word64.
- 생성기 SHA-256: `a7fc34984f6a82c05227abb2efe622f8c49d6ea95c1d94b7d1daf23034efa6d7`.
- 정확한 생성 인자: `GENERATOR.json`. 이 파일의 임시 역사 경로는 provenance 원문이며 runtime 의존성이 아니다.
- 모듈러스: `2736030358979909402780800718157159386076813972158567259200215660948447373041`.
- 생성물: `fiat_q64.go`, SHA-256 `cacf057e59044206a35180851d7de7da60cd02c68df984b9e89e6fd9369b534e`.
- 생성 package: `scalarct`.
- upstream 검토 source commit: `4e6d875bbb5e81fc240a31239f154da3b0b7582e`.
- release binary와 위 source의 hermetic build 동치는 입증하지 않았다. 이 vendoring 경계는 고정 생성물 hash와 제품 차분 검증이다.
- Fiat-Crypto 이름은 Go backend 또는 컴파일 결과의 형식 검증, constant-time 인증을 뜻하지 않는다.
- 라이선스 고지: `COPYRIGHT`, `LICENSE-MIT`, `LICENSE-APACHE`.
