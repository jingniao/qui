package node

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/netaddr"
)

type CertificateLibrary interface {
	// Covering returns issued (valid) library certificates whose names cover domain.
	Covering(ctx context.Context, domain string) ([]LibraryMatch, error)
	CloudflareConfigured(ctx context.Context) (bool, error)
}

type LibraryMatch struct {
	ID       string   `json:"id"`
	Domains  []string `json:"domains"`
	NotAfter string   `json:"notAfter"`
}

type NodeFileMatch struct {
	CertificatePath string   `json:"certificatePath"`
	KeyPath         string   `json:"keyPath"`
	DNSNames        []string `json:"dnsNames"`
	NotAfter        string   `json:"notAfter"`
	Expired         bool     `json:"expired"`
}

type CertificateMatches struct {
	Domain               string          `json:"domain"`
	InventorySupported   bool            `json:"inventorySupported"`
	NodeFiles            []NodeFileMatch `json:"nodeFiles"`
	Library              []LibraryMatch  `json:"library"`
	CloudflareConfigured bool            `json:"cloudflareConfigured"`
}

func coversDomain(names []string, domain string) bool {
	return CoversDomain(names, domain)
}

func CoversDomain(names []string, domain string) bool {
	var cert x509.Certificate
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			cert.IPAddresses = append(cert.IPAddresses, ip)
		} else {
			cert.DNSNames = append(cert.DNSNames, name)
		}
	}
	return cert.VerifyHostname(domain) == nil
}

func (h *Handler) certificateMatches(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := h.Repository.Get(r.Context(), nodeID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		httpapi.Error(w, http.StatusNotFound, "not_found", "Node not found")
		return
	}

	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" || !netaddr.ValidHostName(domain) {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "Valid domain required")
		return
	}

	var inventorySupported bool
	nodeFiles := []NodeFileMatch{}
	if n.Capabilities != nil && n.Capabilities.Host != nil && n.Capabilities.Host.CertificateInventorySupported {
		inventorySupported = true
		now := time.Now().UTC()
		for _, f := range n.Capabilities.Host.CertificateFiles {
			if f == nil || f.KeyPath == "" {
				continue
			}
			names := make([]string, 0, len(f.DnsNames)+len(f.IpAddresses))
			names = append(names, f.DnsNames...)
			names = append(names, f.IpAddresses...)
			if !coversDomain(names, domain) {
				continue
			}
			var expired bool
			var notAfterStr string
			if f.NotAfter != nil {
				t := f.NotAfter.AsTime()
				expired = now.After(t)
				notAfterStr = t.UTC().Format(time.RFC3339)
			}
			nodeFiles = append(nodeFiles, NodeFileMatch{
				CertificatePath: f.CertificatePath,
				KeyPath:         f.KeyPath,
				DNSNames:        f.DnsNames,
				NotAfter:        notAfterStr,
				Expired:         expired,
			})
		}
		sort.SliceStable(nodeFiles, func(i, j int) bool {
			if nodeFiles[i].Expired != nodeFiles[j].Expired {
				return !nodeFiles[i].Expired
			}
			return nodeFiles[i].NotAfter > nodeFiles[j].NotAfter
		})
	}

	library := []LibraryMatch{}
	var cfConfigured bool
	if h.Certificates != nil {
		if lib, err := h.Certificates.Covering(r.Context(), domain); err == nil && lib != nil {
			library = lib
		}
		if configured, err := h.Certificates.CloudflareConfigured(r.Context()); err == nil {
			cfConfigured = configured
		}
	}

	httpapi.JSON(w, http.StatusOK, CertificateMatches{
		Domain:               domain,
		InventorySupported:   inventorySupported,
		NodeFiles:            nodeFiles,
		Library:              library,
		CloudflareConfigured: cfConfigured,
	})
}
