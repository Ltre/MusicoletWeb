package importer

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/blowfish"
)

func TestDecodePayloadBlowfishECB(t *testing.T) {
	plain := []byte(`{"S_P":["/Music/A.mp3"]}`)
	pad := blowfish.BlockSize - len(plain)%blowfish.BlockSize
	input := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher, _ := blowfish.NewCipher([]byte(backupKey))
	encrypted := make([]byte, len(input))
	for off := 0; off < len(input); off += blowfish.BlockSize {
		cipher.Encrypt(encrypted[off:off+blowfish.BlockSize], input[off:off+blowfish.BlockSize])
	}
	got, wasEncrypted, err := decodePayload(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !wasEncrypted || !bytes.Equal(got, plain) {
		t.Fatalf("got %q encrypted=%v", got, wasEncrypted)
	}
}

func TestValidateBackupManifestAndHash(t *testing.T) {
	dir := t.TempDir()
	content := []byte("plain song database")
	contentSum := md5.Sum(content)
	manifest := []byte(`{"files":{"DB_SONGS_LOG":"` + hex.EncodeToString(contentSum[:]) + `"}}`)
	manifestSum := md5.Sum(manifest)
	fixtures := map[string][]byte{
		"DB_SONGS_LOG":       content,
		"0.musicolet.backup": manifest,
		"hash":               []byte(hex.EncodeToString(manifestSum[:])),
	}
	var files []extractedFile
	for name, data := range fixtures {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, extractedFile{name: name, path: path})
	}
	got, err := validateBackup(files)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "VERIFIED" || got.Matched != 1 || got.HashVerified == nil || !*got.HashVerified {
		t.Fatalf("validation=%#v", got)
	}
}

func TestSafeName(t *testing.T) {
	for _, name := range []string{"../x", "/abs", "C:/x"} {
		if _, err := safeName(name); err == nil {
			t.Fatalf("%q accepted", name)
		}
	}
	if got, err := safeName("folder/data.mpl"); err != nil || got != "folder/data.mpl" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestLastPlayedUsesSecondPrecision(t *testing.T) {
	if got := secondsTimestamp(1_777_777_777_987); got != 1_777_777_777 {
		t.Fatalf("got %d", got)
	}
	if got := secondsTimestamp(1_777_777_777); got != 1_777_777_777 {
		t.Fatalf("second timestamp changed: %d", got)
	}
}
