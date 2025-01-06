package config

import (
	"encoding/base64"
	"os"
	"reflect"
	"strings"

	ecies "github.com/ecies/go/v2"
	"github.com/go-errors/errors"
)

type Secret string

func (e *Secret) UnmarshalText(text []byte) error {
	ciphertext, err := maybeLoadEnv(string(text))
	if err != nil {
		return err
	}
	*e, err = EncryptedSecret(ciphertext)
	return err
}

func (e Secret) MarshalText() (text []byte, err error) {
	// TODO: return only hashed values?
	return []byte(e), nil
}

const ENCRYPTED_PREFIX = "encrypted:"

// Decrypt secret values following dotenvx convention:
// https://github.com/dotenvx/dotenvx/blob/main/src/lib/helpers/decryptKeyValue.js
func decrypt(key, value string) (string, error) {
	if !strings.HasPrefix(value, ENCRYPTED_PREFIX) {
		return value, nil
	}
	if len(key) == 0 {
		return value, errors.New("missing private key")
	}
	// Verify private key exists
	privateKey, err := ecies.NewPrivateKeyFromHex(key)
	if err != nil {
		return value, errors.Errorf("failed to hex decode private key: %w", err)
	}
	// Verify ciphertext is base64 encoded
	encoded := value[len(ENCRYPTED_PREFIX):]
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return value, errors.Errorf("failed to base64 decode secret: %w", err)
	}
	// Return decrypted value
	plaintext, err := ecies.Decrypt(privateKey, ciphertext)
	if err != nil {
		return value, errors.Errorf("failed to decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

func EncryptedSecret(ciphertext string) (Secret, error) {
	var err error
	var plaintext string
	key := os.Getenv("DOTENV_PRIVATE_KEY")
	for _, k := range strings.Split(key, ",") {
		plaintext, err = decrypt(k, ciphertext)
		if err == nil && len(plaintext) > 0 {
			break
		}
	}
	return Secret(plaintext), err
}

func DecryptSecretHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}
	if t != reflect.TypeOf(Secret("")) {
		return data, nil
	}
	return EncryptedSecret(data.(string))
}

func (s Secret) Hash(key string) Secret {
	hash := hashPrefix
	if len(s) > 0 {
		hash += sha256Hmac(key, string(s))
	}
	return Secret(hash)
}
