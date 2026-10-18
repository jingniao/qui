//go:build windows

package updater

import "os"

// Windows does not expose portable directory fsync through os.File. Every
// record is FlushFileBuffers'd before rename; abrupt volume-loss acceptance is
// still a separate required platform test, rather than inferred here.
func syncHistoryDirectory(*os.Root) error { return nil }
