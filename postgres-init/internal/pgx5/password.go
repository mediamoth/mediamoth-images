package pgx5

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/mediamoth/mediamoth-images/postgres-init/internal/config"
	"github.com/xdg-go/scram"
)

type PasswordVerifier string

// Hash before constructing DDL so PostgreSQL statement logs never contain
// the plaintext password, including when CREATE USER fails.
func passwordVerifier(password config.Password) (PasswordVerifier, error) {
	client, err := scram.SHA256.NewClient("", string(password), "")
	if err != nil {
		// PostgreSQL uses the original bytes if a password cannot be SASLprepped.
		client, err = scram.SHA256.NewClientUnprepped("", string(password), "")
		if err != nil {
			return "", fmt.Errorf("prepare password verifier")
		}
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	credentials, err := client.GetStoredCredentialsWithError(scram.KeyFactors{Salt: string(salt), Iters: 4096})
	if err != nil {
		return "", fmt.Errorf("generate password verifier")
	}
	encode := base64.StdEncoding.EncodeToString
	return PasswordVerifier(fmt.Sprintf("SCRAM-SHA-256$4096:%s$%s:%s", encode(salt), encode(credentials.StoredKey), encode(credentials.ServerKey))), nil
}
