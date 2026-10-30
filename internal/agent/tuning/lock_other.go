//go:build !linux

package tuning

import (
	"errors"
	"os"
)

func openDesired(root *os.Root) (*os.File, error) { return root.Open("desired.json") }

func lock(string) (func(), error) { return nil, errors.New("tuning helper requires Linux") }
