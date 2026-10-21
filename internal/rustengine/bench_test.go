package rustengine

import (
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/void"
)

// openStore opens a raw store (no encryption) on the named engine.
func openStore(b *testing.B, engine string) void.Store {
	b.Helper()
	s, err := void.OpenStore(engine)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

// BenchmarkStoreWrite compares the storage engines alone: one write of a
// 128-byte record, without encryption or anything else on top.
func BenchmarkStoreWrite(b *testing.B) {
	payload := make([]byte, 128)
	for _, engine := range []string{"kernel", Name} {
		b.Run(engine+"/serial", func(b *testing.B) {
			s := openStore(b, engine)
			b.SetBytes(int64(len(payload)))
			for b.Loop() {
				if _, err := s.Write(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(engine+"/parallel", func(b *testing.B) {
			s := openStore(b, engine)
			b.SetBytes(int64(len(payload)))
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := s.Write(payload); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}

// BenchmarkDeviceWrite compares the engines as the database uses them: the
// write path including the AES-256-GCM → ChaCha20-Poly1305 cascade.
func BenchmarkDeviceWrite(b *testing.B) {
	payload := make([]byte, 128)
	for _, engine := range []string{"kernel", Name} {
		b.Run(engine, func(b *testing.B) {
			void.DefaultEngine = engine
			defer func() { void.DefaultEngine = "kernel" }()
			dev, err := void.Open()
			if err != nil {
				b.Fatal(err)
			}
			defer dev.Close()
			b.SetBytes(int64(len(payload)))
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := dev.Write(payload); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
