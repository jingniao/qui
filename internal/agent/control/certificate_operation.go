package control

import (
	"bytes"
	"context"
	"errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

// RenewCertificateOnly changes only a stopped node's certificate. The existing
// private key, CA trust, ACK and proxy/accounting state are preserved. An
// interrupted renewal is confirmed again by rerunning this command.
func RenewCertificateOnly(ctx context.Context, c config.Agent) (resultErr error) {
	return certificateOperation(ctx, c, false, "certificate-operation")
}
func RotateNodeKeyOnly(ctx context.Context, c config.Agent, version string) error {
	return certificateOperation(ctx, c, true, version)
}
func certificateOperation(ctx context.Context, c config.Agent, rotate bool, version string) (resultErr error) {
	if err := c.Validate(); err != nil {
		return err
	}
	info, err := os.Lstat(c.DataDirectory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("existing node data directory required")
	}
	supervisor, err := lifecycle.TrySupervisor(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, supervisor.Close()) }()
	owner, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.DataDirectory = owner.Directory()
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return err
	}
	call := func(request *pb.CertificateRequest) (*pb.CertificateRenewal, error) {
		tlsConfig, err := transport(c, true)
		if err != nil {
			return nil, err
		}
		connection, err := grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
		if err != nil {
			return nil, err
		}
		defer connection.Close()
		timeout, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return pb.NewAgentControlClient(connection).RenewCertificate(timeout, request)
	}
	if rotate {
		csr, ready, err := state.KeyRotationCSR()
		if err != nil {
			return err
		}
		if !ready {
			response, err := call(&pb.CertificateRequest{CsrPem: csr, Immediate: true, RotateKey: true})
			if err != nil {
				return err
			}
			if !response.KeyRotationSupported || !response.ExplicitRenewalSupported || response.Confirmed {
				return errors.New("central service does not support node key rotation")
			}
			if err := state.StageKeyRotation(response.CertificatePem, response.CaPem); err != nil {
				return err
			}
		}
		if err := state.CommitKeyRotation(version); err != nil {
			return err
		}
	} else {
		csr, err := identity.RenewalCSR(state.Directory)
		if err != nil {
			return err
		}
		response, err := call(&pb.CertificateRequest{CsrPem: csr, Immediate: true})
		if err != nil {
			return err
		}
		if !response.ExplicitRenewalSupported || response.Confirmed || response.ExpiresAt == nil || response.ExpiresAt.CheckValid() != nil {
			return errors.New("central service does not support explicit certificate renewal")
		}
		if err := state.SaveRenewal(response.CertificatePem, response.CaPem); err != nil {
			return err
		}
	}
	// A fresh transport uses the certificate actually persisted on disk, so the
	// server activation is proof of possession of the new local identity.
	response, err := call(&pb.CertificateRequest{ConfirmOnly: true})
	if err != nil {
		return errors.Join(errors.New("local certificate retained; retry with --confirm-node-certificate"), err)
	}
	if !response.ExplicitRenewalSupported || !response.Confirmed {
		return errors.New("central service did not confirm the presented certificate")
	}
	local, key, err := state.Material()
	defer clear(key)
	if err != nil {
		return err
	}
	trusted, err := state.TrustedNodeCA(response.CaPem)
	if err != nil {
		return err
	}
	if !bytes.Equal(local, response.CertificatePem) || !bytes.Equal(trusted, response.CaPem) {
		return errors.New("central certificate confirmation does not match local material")
	}
	if rotate {
		if err := state.FinishKeyRotation(); err != nil {
			return err
		}
	}
	return nil
}
