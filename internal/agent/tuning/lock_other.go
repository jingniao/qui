//go:build !linux

package tuning

import (
	"errors"
	"os"
)

func openDesired(root *os.Root) (*os.File, error)              { return root.Open("desired.json") }
func openRequest(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

func lock(string) (func(), error) { return nil, errors.New("tuning helper requires Linux") }
