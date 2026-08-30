package securestore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

type Store struct {
	db  *sql.DB
	key [32]byte
}

func New(db *sql.DB, masterKey string) (*Store, error) {
	if len(masterKey) < 24 {
		return nil, errors.New("MUSICOLET_MASTER_KEY must be at least 24 characters")
	}
	return &Store{db: db, key: sha256.Sum256([]byte(masterKey))}, nil
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	if key == "" || value == "" {
		return errors.New("secure setting key/value required")
	}
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nil, nonce, []byte(value), []byte(key))
	blob := append(nonce, sealed...)
	_, err = s.db.ExecContext(ctx, `INSERT INTO secure_settings(key,ciphertext,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=excluded.updated_at`, key, blob, time.Now().UnixMilli())
	return err
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	var blob []byte
	if err := s.db.QueryRowContext(ctx, "SELECT ciphertext FROM secure_settings WHERE key=?", key).Scan(&blob); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize() {
		return "", fmt.Errorf("secure setting %q is corrupt", key)
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], []byte(key))
	if err != nil {
		return "", fmt.Errorf("decrypt secure setting %q: %w", key, err)
	}
	return string(plain), nil
}

func (s *Store) Bootstrap(ctx context.Context, key, bootstrap string) error {
	_, err := s.Get(ctx, key)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if bootstrap == "" {
		return fmt.Errorf("secure setting %q is not initialized", key)
	}
	return s.Set(ctx, key, bootstrap)
}
