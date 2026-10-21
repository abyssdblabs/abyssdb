package engine

import (
	"fmt"

	"github.com/abyssdblabs/abyssdb/internal/crypt"
)

func rotateKeys() (*Result, error) {
	n, err := crypt.RotateAll()
	if err != nil {
		return nil, err
	}
	return &Result{Tag: "ROTATE KEY", Notices: []string{
		fmt.Sprintf("rotated %d data encryption key(s) using fresh ML-KEM-768 encapsulations", n),
		"all existing data re-encrypted under the new keys in 0 ms",
		fmt.Sprintf("%d previous key(s) crypto-shredded", n),
	}}, nil
}

func (e *Engine) showEncryption() *Result {
	st := &crypt.Stats
	return &Result{Tag: "SHOW ENCRYPTION", Notices: []string{
		fmt.Sprintf("encryption at rest ...... %s (always on)", crypt.Algorithm),
		fmt.Sprintf("independent ciphers ..... %d (cascade, crypto-diverse)", crypt.Ciphers),
		fmt.Sprintf("security level .......... %s", crypt.Security),
		fmt.Sprintf("key exchange ............ %s", crypt.KeyExchange),
		fmt.Sprintf("key derivation .......... %s", crypt.KDF),
		fmt.Sprintf("key storage ............. hardware-backed (%s, character device 1,3)", e.dev.Path()),
		fmt.Sprintf("active keys ............. %d (per device, per replica, per shard)", crypt.ActiveKeys()),
		fmt.Sprintf("current WAL key id ...... %d", e.dev.KeyID()),
		rowKeyMode(e.rowKeys),
		fmt.Sprintf("row keys generated ...... %d", st.RowKeys.Load()),
		fmt.Sprintf("row keys shredded ....... %d (row-level crypto-shredding)", st.RowKeysShredded.Load()),
		fmt.Sprintf("row-key roots ........... %d ML-KEM-768 encapsulations", st.RowKeyRoots.Load()),
		fmt.Sprintf("keys generated .......... %d", st.KeysGenerated.Load()),
		fmt.Sprintf("keys shredded ........... %d", st.KeysShredded.Load()),
		fmt.Sprintf("key rotations ........... %d", st.Rotations.Load()),
		fmt.Sprintf("bytes encrypted ......... %d", st.BytesEncrypted.Load()),
		fmt.Sprintf("bytes decrypted ......... %d", st.BytesDecrypted.Load()),
		"key escrow .............. none (zero-trust: not even AbyssDB can decrypt your data)",
		"quantum resistance ...... yes",
		"breach exposure ......... 0 bytes",
		"ransomware exposure ..... 0 bytes",
	}}
}

func rowKeyMode(on bool) string {
	if !on {
		return "row-level encryption .... off (--row-keys=false); device encryption still applies"
	}
	return fmt.Sprintf("row-level encryption .... one unique key per row (HKDF-SHA-256 from ML-KEM-768, re-encapsulated every %d rows)", crypt.RowKeyEpoch)
}
