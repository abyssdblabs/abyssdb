package engine

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/chain"
	"github.com/abyssdblabs/abyssdb/internal/nulltime"
)

// onChain submits a committed transaction to AbyssChain and reports any block
// that was mined as a result.
func (e *Engine) onChain(txID string) []string {
	b, err := e.chain.Submit(txID)
	if err != nil {
		return []string{fmt.Sprintf("AbyssChain: block production failed: %v", err)}
	}
	if b == nil {
		return nil
	}
	return []string{fmt.Sprintf("⛏️  AbyssChain block #%d mined: %d txs, hash %s…, %d hashes in %s, signed with ML-DSA-65",
		b.Height, b.TxCount, b.ShortHash(), b.Hashes, b.MineTime.Round(time.Microsecond))}
}

// mint turns the session's most recent result set into an NFT.
func (ss *Session) mint() (*Result, error) {
	if ss.lastQuery == nil {
		return nil, fmt.Errorf("no result set to mint: run a SELECT first")
	}
	cols := selectColumns(ss.lastQuery)
	meta := map[string]any{
		"name":        "Abyss Set",
		"description": "An authentic AbyssDB result set, verified on-chain.",
		"query":       ss.lastQuery.String(),
		"columns":     cols,
		"rows":        0,
		"image":       "data:text/plain,(0 rows)",
		"minted_by":   "nobody",
	}
	b, id, err := ss.e.chain.MintNFT(meta)
	if err != nil {
		return nil, err
	}
	return &Result{Tag: "MINT 1", Notices: []string{
		fmt.Sprintf("🖼️  minted \"Abyss Set #%d\" from: %s", id, ss.lastQuery),
		fmt.Sprintf("token id 0x%s, sealed in AbyssChain block #%d (%s…)", hex.EncodeToString(b.Hash[:8]), b.Height, b.ShortHash()),
		"metadata and artwork stored fully on-chain",
		"floor price: 0.00 ETH · royalties: 0%",
	}}, nil
}

func (e *Engine) showChain() *Result {
	st, mempool := e.chain.Stats()
	verified, verr := e.chain.Verify()
	n := []string{
		"network ................. AbyssChain mainnet (1 validator: nobody)",
		"consensus ............... Proof-of-Absence + SHA-256 proof-of-work",
		"signatures .............. ML-DSA-65 (FIPS 204, post-quantum)",
		fmt.Sprintf("data root ............... %x (Proof-of-Absence commitment)", chain.EmptyRoot[:8]),
		fmt.Sprintf("height .................. %d", e.chain.Head().Height),
		fmt.Sprintf("difficulty .............. %d bits, block size %d txs", e.chain.Difficulty(), e.chain.BlockSize()),
		fmt.Sprintf("blocks mined ............ %d (%d hashes, %s of mining)", st.Blocks, st.Hashes, st.MiningTime.Round(time.Microsecond)),
		fmt.Sprintf("transactions on-chain ... %d (+%d in mempool)", st.Txs-uint64(mempool), mempool),
		fmt.Sprintf("NFTs minted ............. %d", st.NFTs),
		fmt.Sprintf("chain size .............. %d bytes; nodes run as light clients (headers only)", st.BytesOnDisk),
	}
	if verr != nil {
		n = append(n, fmt.Sprintf("verification ............ ❌ %v", verr))
	} else {
		n = append(n, fmt.Sprintf("verification ............ ✅ %d recent blocks: PoW, hash links, Proof-of-Absence, ML-DSA signatures", verified))
	}
	n = append(n, "recent blocks:")
	for _, b := range e.chain.Recent(5) {
		kind := fmt.Sprintf("%d txs", b.TxCount)
		if b.Payload != nil {
			kind += " + payload"
		}
		n = append(n, fmt.Sprintf("  #%-6d %s…  nonce %-8d %s", b.Height, b.ShortHash(), b.Nonce, kind))
	}
	return &Result{Tag: "SHOW CHAIN", Notices: n}
}

func (e *Engine) showRegions() *Result {
	now := time.Now()
	placed := map[string][]string{}
	for _, s := range e.router.Shards {
		placed[s.Region.Name] = append(placed[s.Region.Name], s.Name())
	}
	n := []string{"region             | body       | distance         | one-way light | commit-wait (2ε) | shards"}
	for _, name := range nulltime.Names() {
		r, _ := nulltime.Lookup(name)
		owlt := r.OneWayLightTime(now)
		wait := 2 * owlt
		if r.Terrestrial() {
			wait = 2 * e.coord.Epsilon
		}
		shards := "-"
		if p := placed[name]; len(p) > 0 {
			shards = fmt.Sprint(p)
		}
		n = append(n, fmt.Sprintf("%-18s | %-10s | %-16s | %-13s | %-16s | %s",
			name, r.Body, nulltime.FormatDistance(r.Distance(now)), nulltime.FormatDuration(owlt), nulltime.FormatDuration(wait), shards))
	}
	if nulltime.Warp > 1 {
		n = append(n, fmt.Sprintf("time warp: x%.0f (demo mode: interplanetary waits are compressed)", nulltime.Warp))
	}
	return &Result{Tag: "SHOW REGIONS", Notices: n}
}
