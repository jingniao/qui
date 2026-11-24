//go:build !linux

package hostupdatestate

import "errors"

var errLinuxOnly = errors.New("update state file operations require Linux")

func ReadJournal(file, backups string) (Journal, error) { return Journal{}, errLinuxOnly }
func WriteJournal(snapshot string, j Journal) error {
	if _, err := journalBytes(j); err != nil {
		return err
	}
	return errLinuxOnly
}
func DiscardSnapshot(registry, snapshot string) error  { return errLinuxOnly }
func RemoveDiscarded(registry, discarded string) error { return errLinuxOnly }
