package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
)

type enrollmentObservedFile struct {
	data  []byte
	state string
}
type enrollmentObservation map[string]enrollmentObservedFile

var enrollmentObservedLimits = map[string]int64{
	enrollmentAttemptName: 4096, enrollmentResultName: 128 << 10,
	"identity/client.key": 32768, "identity/client.crt": 32768,
	"identity/node-ca.crt": 32768, "identity/identity.json": 4096,
}

func captureEnrollmentObservation(ctx context.Context, root string) (enrollmentObservation, error) {
	result := enrollmentObservation{}
	for name, limit := range enrollmentObservedLimits {
		if err := ctx.Err(); err != nil {
			result.clear()
			return nil, err
		}
		data, err := readMaterialFile(filepath.Join(root, filepath.FromSlash(name)), limit)
		state := "readable"
		if errors.Is(err, os.ErrNotExist) {
			state = "missing"
		} else if err != nil {
			state = "unavailable"
		}
		result[name] = enrollmentObservedFile{data, state}
	}
	return result, nil
}
func (o enrollmentObservation) clear() {
	for _, item := range o {
		clear(item.data)
	}
}
func (o enrollmentObservation) matches(name string, expected []byte) bool {
	item := o[name]
	return item.state == "readable" && bytes.Equal(item.data, expected)
}
func (o enrollmentObservation) expect(name string, data []byte) {
	old := o[name]
	clear(old.data)
	o[name] = enrollmentObservedFile{append([]byte{}, data...), "readable"}
}
func (o enrollmentObservation) recheck(ctx context.Context, root string) error {
	current, err := captureEnrollmentObservation(ctx, root)
	if err != nil {
		return err
	}
	defer current.clear()
	for name, item := range o {
		next := current[name]
		if item.state != next.state || !bytes.Equal(item.data, next.data) {
			return errors.New("原登记材料或初始目标在读取期间变化，保留材料并重新核对")
		}
	}
	return ctx.Err()
}
