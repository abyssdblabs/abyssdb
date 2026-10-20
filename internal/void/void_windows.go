//go:build windows

package void

import "os"

// On Windows, the Void is the NUL device, a reserved name inherited from
// MS-DOS that cannot be used as a file name in any directory, ever, which is
// the closest thing Windows has to a moral position.
//
// We use the fully qualified device namespace path so that it works even
// from inside a directory that someone has heroically managed to name "NUL".
//
// The Recycle Bin backend (data is lost, but only after the user empties it)
// is available in Deep Abyss, the AbyssDB Enterprise Edition.
const (
	voidPath    = `\\.\NUL`
	backendName = "Abyss kernel-native (Windows)"
)

func openPlatformVoid() (Store, error) { return os.OpenFile(voidPath, os.O_RDWR, 0) }
