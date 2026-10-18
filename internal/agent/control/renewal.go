package control

import (
	"context"
	"errors"
	"proxycontrol/internal/agent/identity"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

var errCertificateRenewed = errors.New("node certificate renewed; reconnecting")

func renewalCheck(state *identity.State, client func() pb.AgentControlClient) func(context.Context) (bool, error) {
	next := time.Time{}
	return func(ctx context.Context) (bool, error) {
		if time.Now().Before(next) {
			return false, nil
		}
		due, err := state.RenewalDue(time.Now())
		if err != nil {
			return false, err
		}
		if !due {
			return false, nil
		}
		if _, err := identity.LoadKeyPair(state.Directory); err != nil {
			return false, errors.New("original node private key is unavailable")
		}
		next = time.Now().Add(time.Hour)
		csr, err := identity.RenewalCSR(state.Directory)
		if err != nil {
			return false, err
		}
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		response, err := client().RenewCertificate(requestCtx, &pb.CertificateRequest{CsrPem: csr})
		if err != nil {
			return false, err
		}
		if response == nil || response.ExpiresAt == nil || response.ExpiresAt.CheckValid() != nil {
			return false, errors.New("invalid certificate expiry in renewal response")
		}
		if err := state.SaveRenewal(response.CertificatePem, response.CaPem); err != nil {
			return false, err
		}
		next = time.Time{}
		return true, nil
	}
}
