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
	"strings"
)

// SecretsMasterKeyEnv is the environment / credentials.config.env key for the
// AES-256 key used to encrypt secrets.enc.json. It is not subject to Prefix.
const SecretsMasterKeyEnv = "AGENTKIT_SECRETS_KEY"

const encryptedSecretsVersion = 1

type encryptedSecretsFile struct {
	Version int                               `json:"version"`
	Entries map[string]encryptedSecretEntry   `json:"entries"`
}

type encryptedSecretEntry struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ParseSecretsMasterKey derives a 32-byte AES key via SHA-256(passphrase).
func ParseSecretsMasterKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("master key is empty")
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

// DecryptSecretsFile parses and decrypts all entries in data.
func DecryptSecretsFile(data []byte, key []byte) (map[string]string, error) {
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	var file encryptedSecretsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse secrets file: %w", err)
	}
	if file.Version != encryptedSecretsVersion {
		return nil, fmt.Errorf("unsupported secrets file version %d", file.Version)
	}
	if file.Entries == nil {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(file.Entries))
	for name, entry := range file.Entries {
		plain, err := decryptEntry(key, entry)
		if err != nil {
			return nil, fmt.Errorf("decrypt %q: %w", name, err)
		}
		out[name] = plain
	}
	return out, nil
}

// EncryptSecretsFile serializes entries to encrypted JSON.
func EncryptSecretsFile(entries map[string]string, key []byte) ([]byte, error) {
	file := encryptedSecretsFile{
		Version: encryptedSecretsVersion,
		Entries: make(map[string]encryptedSecretEntry, len(entries)),
	}
	for name, value := range entries {
		entry, err := encryptEntry(key, value)
		if err != nil {
			return nil, fmt.Errorf("encrypt %q: %w", name, err)
		}
		file.Entries[name] = entry
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// MergeEncryptedSecretsFile decrypts existing (if any), applies updates, and re-encrypts.
func MergeEncryptedSecretsFile(existing []byte, key []byte, updates map[string]string) ([]byte, error) {
	entries, err := DecryptSecretsFile(existing, key)
	if err != nil {
		return nil, err
	}
	for k, v := range updates {
		entries[k] = v
	}
	return EncryptSecretsFile(entries, key)
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
