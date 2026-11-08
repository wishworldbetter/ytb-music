package browsercookies

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"testing"
	"time"
)

// A value Chrome encrypted decrypts to what it was, without the hash of
// its domain that version 24 of the database puts first.
func TestDecrypt(t *testing.T) {
	key, err := pbkdf2.Key(sha1.New, "peanuts", []byte("saltysalt"), 1003, 16)
	if err != nil {
		t.Fatal(err)
	}
	encrypt := func(plain []byte) []byte {
		pad := aes.BlockSize - len(plain)%aes.BlockSize
		plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
		block, _ := aes.NewCipher(key)
		out := make([]byte, len(plain))
		cipher.NewCBCEncrypter(block, bytes.Repeat([]byte(" "), aes.BlockSize)).CryptBlocks(out, plain)
		return append([]byte("v10"), out...)
	}
	hash := sha256.Sum256([]byte(".youtube.com"))
	for _, c := range []struct {
		version int
		plain   []byte
	}{
		{23, []byte("AbC-123/xyz")},
		{24, append(hash[:], "AbC-123/xyz"...)},
	} {
		got, err := decrypt(key, encrypt(c.plain), c.version)
		if err != nil || string(got) != "AbC-123/xyz" {
			t.Errorf("version %d: %q, %v", c.version, got, err)
		}
	}
	if _, err := decrypt(key, []byte("v11abc"), 24); err == nil {
		t.Error("decrypted a value of another scheme")
	}
}

// A cookie of Chrome's expires when Chrome says: its time, in microseconds
// since 1601, overflowed a time.Duration and put every cookie in the past,
// so the app's cookie store deleted them and the sign-in was gone once
// the app opened again.
func TestChromeTime(t *testing.T) {
	// 2027-03-01 12:00 UTC, as Chrome keeps it.
	if got, want := chromeTime(13_448_376_000_000_000), time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("%v, want %v", got, want)
	}
}
