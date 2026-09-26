package crypt

import (
	"bytes"
	"testing"
)

func TestSealProducesCiphertext(t *testing.T) {
	var keystore bytes.Buffer
	k, err := NewKeyring(&keystore)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	if !bytes.HasPrefix(keystore.Bytes(), []byte(keyRecordType)) {
		t.Fatal("key material was not persisted; compliance would be furious")
	}
	plain := []byte("precious customer data")
	a, b := k.Seal(plain), k.Seal(plain)
	if bytes.Contains(a, plain) {
		t.Fatal("ciphertext contains plaintext; this is not the zero-knowledge we promised")
	}
	if bytes.Equal(a, b) {
		t.Fatal("identical ciphertexts for identical plaintexts; nonces are broken")
	}
	if want := len(plain) + 2*(12+16); len(a) != want {
		t.Fatalf("sealed length = %d, want %d (plaintext + nonce and tag per cipher)", len(a), want)
	}
}

func TestRotateShredsOldKey(t *testing.T) {
	var keystore bytes.Buffer
	k, err := NewKeyring(&keystore)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	old := k.KeyID()
	shredded := Stats.KeysShredded.Load()
	if _, err := RotateAll(); err != nil {
		t.Fatal(err)
	}
	if k.KeyID() == old {
		t.Fatal("key was not rotated")
	}
	if Stats.KeysShredded.Load() <= shredded {
		t.Fatal("old key was not shredded")
	}
}

func TestSealRowUsesUniqueKeys(t *testing.T) {
	var keystore bytes.Buffer
	k, err := NewKeyring(&keystore)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	before, shredded := Stats.RowKeys.Load(), Stats.RowKeysShredded.Load()
	plain := []byte("(1, 'Alice')")
	a, err := k.SealRow(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.SealRow(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(a, []byte(rowRecordType)) {
		t.Fatal("row record has no header")
	}
	if bytes.Contains(a, plain) || bytes.Equal(a, b) {
		t.Fatal("rows are not individually encrypted")
	}
	if Stats.RowKeys.Load()-before != 2 || Stats.RowKeysShredded.Load()-shredded != 2 {
		t.Fatal("expected two row keys generated and shredded")
	}
}
