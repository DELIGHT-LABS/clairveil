package reservation

import "testing"

func TestNullifierLookupKeyDeterministicAndKeyed(t *testing.T) {
	first, err := NullifierLookupKey([]byte("index-key-a"), []byte("nullifier"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NullifierLookupKey([]byte("index-key-a"), []byte("nullifier"))
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := NullifierLookupKey([]byte("index-key-b"), []byte("nullifier"))
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("expected deterministic lookup key")
	}
	if first == otherKey {
		t.Fatalf("expected different index keys to produce different lookup keys")
	}
}
