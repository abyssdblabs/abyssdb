package chain

import (
	"io"
	"testing"
)

func TestMiningAndVerification(t *testing.T) {
	c, err := New(io.Discard, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	var mined int
	for i := 0; i < 10; i++ {
		b, err := c.Submit("tx")
		if err != nil {
			t.Fatal(err)
		}
		if b != nil {
			mined++
			if leadingZeroBits(b.Hash) < 8 {
				t.Fatalf("block %d has insufficient proof-of-work", b.Height)
			}
		}
	}
	if mined != 2 {
		t.Fatalf("mined %d blocks, want 2", mined)
	}
	if _, _, err := c.MintNFT(map[string]any{"rows": 0}); err != nil {
		t.Fatal(err)
	}
	n, err := c.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 { // genesis + 2 + NFT
		t.Fatalf("verified %d blocks, want 4", n)
	}
	if c.Head().DataRoot != EmptyRoot {
		t.Fatal("Proof-of-Absence violated")
	}
}

func TestTamperingIsDetected(t *testing.T) {
	c, err := New(io.Discard, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Submit("tx"); err != nil {
		t.Fatal(err)
	}
	c.recent[1].DataRoot[0] ^= 0xff // someone tried to store data
	if _, err := c.Verify(); err == nil {
		t.Fatal("tampering went undetected")
	}
}
