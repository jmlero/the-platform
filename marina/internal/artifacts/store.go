package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const MaxBytes = 4 << 20

type Store struct{ root string }

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	return &Store{root: absolute}, nil
}
func Digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (store *Store) name(digest string) (string, error) {
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	if len(hexDigest) != 64 || digest != "sha256:"+hexDigest || strings.ToLower(hexDigest) != hexDigest {
		return "", errors.New("invalid artifact digest")
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return "", errors.New("invalid artifact digest")
	}
	return filepath.Join(store.root, hexDigest), nil
}
func (store *Store) Put(value []byte) (string, error) {
	if len(value) > MaxBytes {
		return "", errors.New("artifact exceeds 4 MiB")
	}
	digest := Digest(value)
	name, _ := store.name(digest)
	if _, err := os.Stat(name); err == nil {
		_, err = store.Get(digest)
		return digest, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	file, err := os.CreateTemp(store.root, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(value); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Link(file.Name(), name); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	// Flush the directory entry before publishing the digest in the journal.
	directory, err := os.Open(store.root)
	if err != nil {
		return "", err
	}
	err = directory.Sync()
	_ = directory.Close()
	if err != nil {
		return "", err
	}
	_, err = store.Get(digest)
	return digest, err
}
func (store *Store) Get(digest string) ([]byte, error) {
	name, err := store.name(digest)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("artifact is not a regular file")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(value) > MaxBytes || Digest(value) != digest {
		return nil, fmt.Errorf("artifact failed integrity verification")
	}
	return value, nil
}
