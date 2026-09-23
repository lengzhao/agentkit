package credentials

import (
	"encoding/json"
	"testing"
)

func TestMergeEncryptedSecretsFileForAddPreservesAbnormal(t *testing.T) {
	t.Parallel()
	goodKey, err := parseSecretsMasterKey("good-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	badKey, err := parseSecretsMasterKey("bad-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	goodBlob, err := encryptSecretsFile(map[string]string{
		"keep::KEY": "kept",
	}, goodKey)
	if err != nil {
		t.Fatal(err)
	}
	badBlob, err := encryptSecretsFile(map[string]string{
		"stale::KEY": "gone",
	}, badKey)
	if err != nil {
		t.Fatal(err)
	}
	var goodFile encryptedSecretsFile
	var badFile encryptedSecretsFile
	if err := json.Unmarshal(goodBlob, &goodFile); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(badBlob, &badFile); err != nil {
		t.Fatal(err)
	}
	combined := encryptedSecretsFile{
		Version: encryptedSecretsVersion,
		Entries: map[string]encryptedSecretEntry{},
	}
	for k, v := range goodFile.Entries {
		combined.Entries[k] = v
	}
	for k, v := range badFile.Entries {
		combined.Entries[k] = v
	}
	combinedBytes, err := json.Marshal(combined)
	if err != nil {
		t.Fatal(err)
	}

	merged, warnings, overwrites, err := mergeEncryptedSecretsFileForAdd(combinedBytes, goodKey, map[string]string{
		"new::KEY": "added",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overwrites) != 0 {
		t.Fatalf("overwrites=%v", overwrites)
	}
	if len(warnings) != 1 || warnings[0].name != "stale::KEY" {
		t.Fatalf("warnings=%v", warnings)
	}
	var outFile encryptedSecretsFile
	if err := json.Unmarshal(merged, &outFile); err != nil {
		t.Fatal(err)
	}
	stale, ok := outFile.Entries["stale::KEY"]
	if !ok || !stale.Abnormal {
		t.Fatalf("stale entry=%v, want abnormal preserved", stale)
	}
	got, err := decryptSecretsFile(merged, goodKey)
	if err != nil {
		t.Fatal(err)
	}
	if got["keep::KEY"] != "kept" || got["new::KEY"] != "added" {
		t.Fatalf("got=%v", got)
	}
	if _, ok := got["stale::KEY"]; ok {
		t.Fatal("stale should not be in plaintext map")
	}
}

func TestMergeEncryptedSecretsFileForAddOverwriteReplacesUndecryptable(t *testing.T) {
	t.Parallel()
	goodKey, err := parseSecretsMasterKey("good-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	badKey, err := parseSecretsMasterKey("bad-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	badBlob, err := encryptSecretsFile(map[string]string{
		"scope::TOKEN": "old",
	}, badKey)
	if err != nil {
		t.Fatal(err)
	}

	merged, warnings, overwrites, err := mergeEncryptedSecretsFileForAdd(badBlob, goodKey, map[string]string{
		"scope::TOKEN": "fresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overwrites) != 1 || overwrites[0] != "scope::TOKEN" {
		t.Fatalf("overwrites=%v", overwrites)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v, want none when overwriting same key", warnings)
	}
	got, err := decryptSecretsFile(merged, goodKey)
	if err != nil {
		t.Fatal(err)
	}
	if got["scope::TOKEN"] != "fresh" {
		t.Fatalf("got=%v", got)
	}
	var outFile encryptedSecretsFile
	if err := json.Unmarshal(merged, &outFile); err != nil {
		t.Fatal(err)
	}
	entry := outFile.Entries["scope::TOKEN"]
	if entry.Abnormal {
		t.Fatal("replaced entry should not be abnormal")
	}
}
