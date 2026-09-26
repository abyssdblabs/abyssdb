//go:build !unix

package engine

// processCPUSeconds is unavailable here; the Go runtime estimate is used.
func processCPUSeconds() (float64, bool) { return 0, false }
