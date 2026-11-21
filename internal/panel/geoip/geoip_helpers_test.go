package geoip

// Version is the data release embedded in this binary (for example "2026-11"),
// or "" when the table cannot be read.
func Version() string {
	t, err := current()
	if err != nil {
		return ""
	}
	return t.version
}

// Err reports a damaged embedded table; Country then always returns "".
func Err() error {
	_, err := current()
	return err
}
