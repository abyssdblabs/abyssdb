package rustengine

import (
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/void"
)

func TestRustEngineBacksTheDatabase(t *testing.T) {
	void.DefaultEngine = Name
	defer func() { void.DefaultEngine = "kernel" }()

	dev, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if dev.Backend() != backend {
		t.Fatalf("backend = %q", dev.Backend())
	}
	eng, err := engine.New(dev, engine.Options{ChainDifficulty: 4})
	if err != nil {
		t.Fatal(err)
	}
	results, err := eng.ExecString(`
		CREATE TABLE t (id INT, name TEXT);
		INSERT INTO t VALUES (1, 'a'), (2, 'b');
		SELECT * FROM t;
		ROTATE KEY;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[2].Rows) != 0 {
		t.Fatal("rows returned")
	}
	if dev.Stats.BytesWritten.Load() == 0 {
		t.Fatal("nothing was written through the Rust engine")
	}

	// Several devices share the module, each with its own handle.
	other, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	other.Close()
}
