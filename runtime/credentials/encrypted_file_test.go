package credentials

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testMasterKey(t *testing.T) []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestParseSecretsMasterKeyBase64(t *testing.T) {
	key := testMasterKey(t)
	got, err := ParseSecretsMasterKey(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(key) {
		t.Fatal("key mismatch")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testMasterKey(t)
	updates := map[string]string{
		"OPENAI_API_KEY": "sk-test",
		"GITHUB_TOKEN":   "ghp_x",
	}
	data, err := EncryptSecretsFile(updates, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptSecretsFile(data, key)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range updates {
		if got[k] != want {
			t.Fatalf("%s=%q, want %q", k, got[k], want)
		}
	}
}

func TestMergeEncryptedSecretsFile(t *testing.T) {
	key := testMasterKey(t)
	first, err := EncryptSecretsFile(map[string]string{"A": "1"}, key)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeEncryptedSecretsFile(first, key, map[string]string{"B": "2"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptSecretsFile(merged, key)
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "1" || got["B"] != "2" {
		t.Fatalf("got %#v", got)
	}
	merged, err = MergeEncryptedSecretsFile(merged, key, map[string]string{"A": "3"})
	if err != nil {
		t.Fatal(err)
	}
	got, err = DecryptSecretsFile(merged, key)
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "3" {
		t.Fatalf("A=%q, want 3", got["A"])
	}
}
