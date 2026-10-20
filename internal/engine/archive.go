package engine

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/noctarius/abyssdb/internal/nulltime"
	"github.com/noctarius/abyssdb/internal/sql"
	"github.com/noctarius/abyssdb/internal/void"
)

// The Deep Abyss Archive is AbyssDB's cold storage tier: backups are stored in
// the DEEP_ABYSS storage class, modeled on the coldest tier of a certain
// well-known cloud's object storage. Backups complete instantly. Restores take
// 12 hours, because premium nothing should feel premium.

const (
	archiveScheme      = "deep-abyss://"
	restoreThawTime    = 12 * time.Hour
	minStorageDuration = 180 * 24 * time.Hour
)

// The earliest recoverable point: the beginning of Unix time. (/dev/null
// itself only appeared in print on 1971-11-03, in the Unix Programmer's
// Manual, First Edition, so the first 22 months are restored from memory.)
var earliestRecoverable = time.Unix(0, 0).UTC()

type backupRecord struct {
	ID         int
	URI        string
	Created    time.Time
	Compressed int
}

type restoreJob struct {
	ID      int
	URI     string
	AsOf    time.Time
	Started time.Time
	ReadyAt time.Time // warped
}

// archive is the backup catalog. It lives in RAM, making it the only state
// AbyssDB actually keeps, and therefore the thing most likely to be lost.
type archive struct {
	mu       sync.Mutex
	backups  []backupRecord
	restores []restoreJob
}

func (e *Engine) requireDeepAbyss(feature string) error {
	if e.edition == EditionDeepAbyss {
		return nil
	}
	return fmt.Errorf("%s requires Deep Abyss, the AbyssDB Enterprise Edition (--edition deep-abyss)", feature)
}

func (e *Engine) backup(s *sql.BackupStmt) (*Result, error) {
	if err := e.requireDeepAbyss("BACKUP"); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(s.Target, archiveScheme) {
		return nil, fmt.Errorf("unsupported backup target %q: the Deep Abyss Archive only accepts %s<bucket>/<path>", s.Target, archiveScheme)
	}

	// 1. Consistent snapshot of every device: the WAL and every shard.
	devices := 1 + len(e.router.Shards)
	var snapshot bytes.Buffer
	data, err := e.dev.ReadAll()
	if err != nil {
		return nil, err
	}
	snapshot.Write(data)
	if _, _, err := e.router.VerifyConsistency(); err != nil { // reads every replica too
		return nil, err
	}

	// 2. Compress. gzip is a real compressor; it can do nothing about nothing.
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(snapshot.Bytes())
	_ = w.Close()

	// 3. Encrypt and upload to the archive (a dedicated, encrypted void).
	dev, err := void.Open()
	if err != nil {
		return nil, err
	}
	defer dev.Close()
	if _, err := dev.Write(gz.Bytes()); err != nil {
		return nil, err
	}
	_ = dev.Fsync()

	e.archive.mu.Lock()
	id := len(e.archive.backups) + 1
	e.archive.backups = append(e.archive.backups, backupRecord{id, s.Target, time.Now(), gz.Len()})
	e.archive.mu.Unlock()

	return &Result{Tag: "BACKUP", Notices: []string{
		fmt.Sprintf("snapshot: %d device(s) and all replicas, consistency verified", devices),
		fmt.Sprintf("compressed %d bytes to %d bytes (gzip)", snapshot.Len(), gz.Len()),
		"encrypted with AES-256-GCM → ChaCha20-Poly1305 under a fresh ML-KEM-768 key",
		fmt.Sprintf("uploaded to %s (storage class DEEP_ABYSS, backup id %d)", s.Target, id),
		"pricing: $0.00099/GB-month, 180-day minimum storage duration; estimated monthly cost: $0.00",
		"restore SLA: 12 hours (standard retrieval)",
	}}, nil
}

func (e *Engine) restore(s *sql.RestoreStmt) (*Result, error) {
	if err := e.requireDeepAbyss("RESTORE"); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(s.Source, archiveScheme) {
		return nil, fmt.Errorf("unsupported restore source %q: expected %s<bucket>/<path>", s.Source, archiveScheme)
	}
	r := &Result{Tag: "RESTORE"}
	note := func(f string, a ...any) { r.Notices = append(r.Notices, fmt.Sprintf(f, a...)) }

	asOf := time.Now()
	if s.AsOf != "" {
		t, err := parseTimestamp(s.AsOf)
		if err != nil {
			return nil, err
		}
		switch {
		case t.Before(earliestRecoverable):
			return nil, fmt.Errorf("cannot restore to %s: the earliest recoverable point is %s",
				t.Format("2006-01-02"), earliestRecoverable.Format("2006-01-02"))
		case t.Equal(earliestRecoverable):
			note("point-in-time recovery to %s (earliest recoverable point: the beginning of Unix time)", t.Format("2006-01-02"))
		case t.After(time.Now()):
			note("point-in-time recovery to %s (future timestamps are supported)", t.Format("2006-01-02"))
		default:
			note("point-in-time recovery to %s", t.Format(time.RFC3339))
		}
		asOf = t
	}

	now := time.Now()
	e.archive.mu.Lock()
	id := len(e.archive.restores) + 1
	job := restoreJob{id, s.Source, asOf, now, now.Add(nulltime.Warped(restoreThawTime))}
	e.archive.restores = append(e.archive.restores, job)
	found := false
	for _, b := range e.archive.backups {
		found = found || b.URI == s.Source
	}
	e.archive.mu.Unlock()

	if !found {
		note("backup %s not found in the catalog; proceeding with catalog-independent recovery", s.Source)
	}
	note("restore job %d initiated: thawing from DEEP_ABYSS (standard retrieval)", id)
	note("ETA: %s. Check progress with SHOW BACKUPS.", nulltime.FormatDuration(job.ReadyAt.Sub(now)))
	note("the database remains fully available during the restore")
	return r, nil
}

func (e *Engine) showBackups() *Result {
	e.archive.mu.Lock()
	defer e.archive.mu.Unlock()
	n := []string{fmt.Sprintf("edition: %s", e.edition)}
	if len(e.archive.backups) == 0 && len(e.archive.restores) == 0 {
		n = append(n, "no backups or restores in this session")
	}
	for _, b := range e.archive.backups {
		n = append(n, fmt.Sprintf("backup %d: %s, %d bytes, DEEP_ABYSS, created %s, early deletion fee applies until %s",
			b.ID, b.URI, b.Compressed, b.Created.Format(time.TimeOnly), b.Created.Add(minStorageDuration).Format("2006-01-02")))
	}
	now := time.Now()
	for _, j := range e.archive.restores {
		total := j.ReadyAt.Sub(j.Started)
		if now.Before(j.ReadyAt) {
			pct := float64(now.Sub(j.Started)) / float64(total) * 100
			n = append(n, fmt.Sprintf("restore %d: %s thawing... %.1f%% (ETA %s)", j.ID, j.URI, pct, nulltime.FormatDuration(j.ReadyAt.Sub(now))))
		} else {
			n = append(n, fmt.Sprintf("restore %d: %s ✅ complete (as of %s); database state verified",
				j.ID, j.URI, j.AsOf.Format("2006-01-02")))
		}
	}
	return &Result{Tag: "SHOW BACKUPS", Notices: n}
}

func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q (try YYYY-MM-DD)", s)
}
