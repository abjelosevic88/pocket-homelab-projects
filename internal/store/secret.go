package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Secrets kept for a person (their API token on a connected instance) are
// sealed with AES-GCM under a key that lives in its own file beside the
// database, never in it: a copy of the database, a backup download or a
// database browser shows only "enc1:…". Losing the key file loses those
// secrets (the person pastes a new token); nothing else depends on it.
const sealedPrefix = "enc1:"

// UseSecretKey loads the key file (creating it, 0600, on first start) and
// seals the connection tokens still stored in plain text.
func (s *Store) UseSecretKey(ctx context.Context, path string) error {
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	} else if err != nil {
		return err
	}
	if len(key) != 32 {
		return fmt.Errorf("%s: want 32 bytes, found %d", path, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	if s.aead, err = cipher.NewGCM(block); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, token FROM remotes WHERE token NOT LIKE 'enc1:%'`)
	if err != nil {
		return err
	}
	plain := map[int64]string{}
	for rows.Next() {
		var id int64
		var tok string
		if err := rows.Scan(&id, &tok); err != nil {
			rows.Close()
			return err
		}
		plain[id] = tok
	}
	rows.Close()
	for id, tok := range plain {
		if _, err := s.DB.ExecContext(ctx, `UPDATE remotes SET token = ? WHERE id = ?`, s.seal(tok), id); err != nil {
			return err
		}
	}
	if len(plain) > 0 {
		s.log.Info("sealed connection tokens", "count", len(plain))
	}
	return nil
}

func (s *Store) seal(v string) string {
	if s.aead == nil || v == "" || strings.HasPrefix(v, sealedPrefix) {
		return v
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err) // no randomness: nothing safe to do
	}
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(v), nil))
}

// unseal gives "" for a sealed value it can't open (other or missing key).
func (s *Store) unseal(v string) string {
	if !strings.HasPrefix(v, sealedPrefix) {
		return v
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(v, sealedPrefix))
	if s.aead == nil || err != nil || len(b) < s.aead.NonceSize() {
		return ""
	}
	n := s.aead.NonceSize()
	out, err := s.aead.Open(nil, b[:n], b[n:], nil)
	if err != nil {
		return ""
	}
	return string(out)
}
