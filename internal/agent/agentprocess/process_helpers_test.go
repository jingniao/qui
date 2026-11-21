package agentprocess

import "io"

func Confirm(r Request, node string, revision int64, version string, output io.Writer) error {
	return ConfirmHealth(r, node, revision, version, true, output)
}
