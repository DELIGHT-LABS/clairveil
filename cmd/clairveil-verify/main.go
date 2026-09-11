package main

import (
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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

	// Seed derivation is distinct from strict persisted-private-key import.
	scalar, err := crypto.DeriveIdentityScalarSeed32(seed)
	if err != nil {
		fmt.Printf("Key derivation failed: %v\n", err)
		os.Exit(1)
	}

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

	defer clear(plaintext)
	if err := writeVerifiedNote(os.Stdout, plaintext); err != nil {
		fmt.Printf("Invalid NotePlaintextV1: %v\n", err)
		os.Exit(1)
	}
}

// writeVerifiedNote is an explicit display boundary; decrypted recovery
// fields remain fixed-width and private randomness is not formatted or logged.
func writeVerifiedNote(w io.Writer, plaintext []byte) error {
	note, err := types.UnmarshalSecretNotePlaintextV1(plaintext)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Decryption successful\nAmount: %d\nAsset: %s\nMemo: %s\n", note.Amount, note.AssetIDHex(), note.Memo)
	return err
}
