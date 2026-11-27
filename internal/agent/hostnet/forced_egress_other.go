//go:build !linux

package hostnet

import "errors"

func CheckForcedEgressMark() error {
	return errors.New("managed egress with a forced source requires Linux")
}
