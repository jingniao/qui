package hostnet

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// CheckForcedEgressMark checks the Agent's inherited permissions, which its
// core children also receive. An unsupported kernel or service configuration
// must fail before publishing a core that cannot mark its outbound sockets.
func CheckForcedEgressMark() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open managed egress mark probe: %w", err)
	}
	err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, ForcedEgressMark)
	if err != nil {
		err = fmt.Errorf("managed egress needs SO_MARK support and the Agent's CAP_NET_RAW: %w", err)
	}
	return errors.Join(err, unix.Close(fd))
}
