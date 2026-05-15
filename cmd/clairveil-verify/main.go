package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"

	// 프로젝트 경로에 맞춰 수정해주세요
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

func main() {
	// 플래그 파싱
	encStr := flag.String("enc", "", "Base64 Encrypted String (CipherText)")
	secretStr := flag.String("secret", "", "Secret Key Seed (Address String)")
	flag.Parse()

	if *encStr == "" || *secretStr == "" {
		fmt.Println("Usage: ./clairveil-verify -enc [BASE64_STRING] -secret [ADDRESS_OR_SEED]")
		os.Exit(1)
	}

	fmt.Println("🔍 Privacy Note Verifier (ECIES Updated)")
	fmt.Println("------------------------------------------------")

	// 1. Secret Key 유도 (CLI의 getExplicitKeys 로직과 동일해야 함)
	// Seed = SHA256(AddressString)
	seed := sha256.Sum256([]byte(*secretStr))

	// Scalar(PrivateKey) 생성
	scalar := new(big.Int).SetBytes(seed[:])
	curve := twistededwards.GetEdwardsCurve()
	scalar.Mod(scalar, &curve.Order) // Curve Order 안으로 맞춤

	fmt.Printf("🔑 Derived Private Key (Scalar): %s...\n", scalar.String()[:10])

	// 2. Base64 Decoding
	cipherBytes, err := base64.StdEncoding.DecodeString(*encStr)
	if err != nil {
		fmt.Printf("❌ Base64 Decode Failed: %v\n", err)
		os.Exit(1)
	}

	// 3. Decryption (ECIES)
	fmt.Println("🔓 Attempting Asymmetric Decryption...")
	plaintext, err := crypto.AsymDecrypt(cipherBytes, scalar)
	if err != nil {
		fmt.Printf("❌ Decryption Failed!\n")
		fmt.Printf("   Reason: %v\n", err)
		fmt.Println("   Tip: Ensure the sender used YOUR public key derived from this address.")
		os.Exit(1)
	}

	// 4. 결과 출력
	fmt.Println("✅ Decryption Successful!")
	fmt.Println("------------------------------------------------")
	fmt.Printf("📄 Raw JSON: %s\n", string(plaintext))

	// 5. JSON 파싱 확인
	var note types.Note
	if err := json.Unmarshal(plaintext, &note); err == nil {
		fmt.Println("------------------------------------------------")
		fmt.Printf("💰 Amount:     %s\n", note.Amount.String())
		fmt.Printf("🎲 Randomness: %s\n", note.Randomness.String())
		fmt.Printf("📝 Memo:       %s\n", note.Memo)
		fmt.Println("------------------------------------------------")
	}
}
