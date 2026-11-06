package node

import (
	"bytes"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

const deviceGateRefreshInterval = time.Minute

// The Agent may report immediately after a command. Sending the same gate after
// every report creates a command/heartbeat feedback loop. Changes still go out
// immediately; unchanged gates renew well before their three-minute expiry.
type deviceGateDelivery struct {
	payload []byte
	sentAt  time.Time
}

func (d *deviceGateDelivery) send(command *pb.DeviceGateCommand, now time.Time, send func(any) error) error {
	if !d.sentAt.IsZero() && bytes.Equal(d.payload, command.RequestJson) && now.Sub(d.sentAt) < deviceGateRefreshInterval {
		return nil
	}
	if err := send(command); err != nil {
		return err
	}
	d.payload = append(d.payload[:0], command.RequestJson...)
	d.sentAt = now
	return nil
}
