package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// SecretsMasterKeyEnv is the environment / credentials.config.env key for the
// AES-256 key used to encrypt secrets.enc.json. It is not subject to Prefix.
const SecretsMasterKeyEnv = "AGENTKIT_SECRETS_KEY"

const encryptedSecretsVersion = 1

type encryptedSecretsFile struct {
	Version int                             `json:"version"`
	Entries map[string]encryptedSecretEntry `json:"entries"`
}

type encryptedSecretEntry struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	// Abnormal marks entries that could not be decrypted with the current master
	// key; ciphertext is preserved until the key is fixed or the entry is re-added.
	Abnormal bool `json:"abnormal,omitempty"`
}

func parseSecretsMasterKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("master key is empty")
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func parseEncryptedSecretsFile(data []byte) (encryptedSecretsFile, error) {
	if len(data) == 0 {
		return encryptedSecretsFile{Version: encryptedSecretsVersion, Entries: map[string]encryptedSecretEntry{}}, nil
	}
	var file encryptedSecretsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return encryptedSecretsFile{}, fmt.Errorf("parse secrets file: %w", err)
	}
	if file.Version != encryptedSecretsVersion {
		return encryptedSecretsFile{}, fmt.Errorf("unsupported secrets file version %d", file.Version)
	}
	if file.Entries == nil {
		file.Entries = map[string]encryptedSecretEntry{}
	}
	return file, nil
}

// loadEncryptedSecretsFile loads decryptable values and lists abnormal entry names.
// Decrypt failures do not return an error; those keys are reported as abnormal.
func loadEncryptedSecretsFile(data []byte, key []byte) (map[string]string, []string, error) {
	file, err := parseEncryptedSecretsFile(data)
	if err != nil {
		return nil, nil, err
	}
	plain := make(map[string]string, len(file.Entries))
	var abnormal []string
	for name, entry := range file.Entries {
		if entry.Abnormal {
			abnormal = append(abnormal, name)
			continue
		}
		value, err := decryptEntry(key, entry)
		if err != nil {
			abnormal = append(abnormal, name)
			continue
		}
		plain[name] = value
	}
	sort.Strings(abnormal)
	return plain, abnormal, nil
}

func decryptSecretsFile(data []byte, key []byte) (map[string]string, error) {
	plain, _, err := loadEncryptedSecretsFile(data, key)
	return plain, err
}

func encryptSecretsFile(entries map[string]string, key []byte) ([]byte, error) {
	return writeEncryptedSecretsFile(entries, nil, key)
}

func writeEncryptedSecretsFile(plain map[string]string, abnormal map[string]encryptedSecretEntry, key []byte) ([]byte, error) {
	file := encryptedSecretsFile{
		Version: encryptedSecretsVersion,
		Entries: make(map[string]encryptedSecretEntry, len(plain)+len(abnormal)),
	}
	for name, value := range plain {
		entry, err := encryptEntry(key, value)
		if err != nil {
			return nil, fmt.Errorf("encrypt %q: %w", name, err)
		}
		file.Entries[name] = entry
	}
	for name, entry := range abnormal {
		if _, exists := file.Entries[name]; exists {
			continue
		}
		entry.Abnormal = true
		file.Entries[name] = entry
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func mergeEncryptedSecretsFile(existing []byte, key []byte, updates map[string]string) ([]byte, error) {
	merged, warnings, _, err := mergeEncryptedSecretsFileForAdd(existing, key, updates)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		return nil, fmt.Errorf("decrypt %q: %v", warnings[0].name, warnings[0].err)
	}
	return merged, nil
}

type encryptMergeWarning struct {
	name string
	err  error
}

// mergeEncryptedSecretsFileForAdd merges updates (overwriting same keys). Entries
// that cannot be decrypted are kept on disk with abnormal=true unless the same
// key appears in updates (re-add replaces the entry).
func mergeEncryptedSecretsFileForAdd(existing []byte, key []byte, updates map[string]string) ([]byte, []encryptMergeWarning, []string, error) {
	file, err := parseEncryptedSecretsFile(existing)
	if err != nil {
		return nil, nil, nil, err
	}
	var overwrites []string
	for k := range updates {
		if _, ok := file.Entries[k]; ok {
			overwrites = append(overwrites, k)
		}
	}
	sort.Strings(overwrites)

	plain, abnormalEntries, warnings, err := classifyEncryptedSecretsFile(existing, key)
	if err != nil {
		return nil, nil, nil, err
	}
	for k, v := range updates {
		delete(abnormalEntries, k)
		plain[k] = v
	}
	filtered := warnings[:0]
	for _, w := range warnings {
		if _, ok := updates[w.name]; ok {
			continue
		}
		filtered = append(filtered, w)
	}
	merged, err := writeEncryptedSecretsFile(plain, abnormalEntries, key)
	if err != nil {
		return nil, nil, nil, err
	}
	return merged, filtered, overwrites, nil
}

func classifyEncryptedSecretsFile(data []byte, key []byte) (map[string]string, map[string]encryptedSecretEntry, []encryptMergeWarning, error) {
	file, err := parseEncryptedSecretsFile(data)
	if err != nil {
		return nil, nil, nil, err
	}
	plain := make(map[string]string, len(file.Entries))
	abnormalEntries := make(map[string]encryptedSecretEntry)
	var warnings []encryptMergeWarning
	for name, entry := range file.Entries {
		if entry.Abnormal {
			abnormalEntries[name] = entry
			continue
		}
		value, err := decryptEntry(key, entry)
		if err != nil {
			entry.Abnormal = true
			abnormalEntries[name] = entry
			warnings = append(warnings, encryptMergeWarning{name: name, err: err})
			continue
		}
		plain[name] = value
	}
	return plain, abnormalEntries, warnings, nil
}

func encryptEntry(key []byte, plaintext string) (encryptedSecretEntry, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return encryptedSecretEntry{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedSecretEntry{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedSecretEntry{}, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return encryptedSecretEntry{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptEntry(key []byte, entry encryptedSecretEntry) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(entry.Nonce)
	if err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(entry.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("ciphertext: %w", err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
