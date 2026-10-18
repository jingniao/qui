//go:build windows

package restoretasks

// Windows has no portable directory fsync through os.File. The original
// file Sync is retained; do not claim a verified directory durability floor.
func syncProgressDirectory(string) error { return nil }
