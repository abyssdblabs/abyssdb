package rustengine

import (
	"sync"
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/void"
)

// Stores must not share a WebAssembly instance: concurrent writes from several
// stores used to corrupt the Rust allocator and trap with "unreachable".
func TestConcurrentStores(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		s, err := void.OpenStore(Name)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := make([]byte, 64+i*97)
			for j := 0; j < 20000; j++ {
				n, err := s.Write(p)
				if err != nil || n != len(p) {
					errs <- err
					return
				}
				if j%100 == 0 {
					if err := s.Sync(); err != nil {
						errs <- err
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("write failed: %v", err)
	}
}
