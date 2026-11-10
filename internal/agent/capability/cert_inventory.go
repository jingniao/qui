package capability

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

const maxCertificateFileSize = 1024 * 1024 // 1 MiB
const maxCertificateSummaries = 64

type keyCandidate struct {
	filename string
	path     string
	data     []byte
}

type certCandidate struct {
	filename    string
	path        string
	data        []byte
	leaf        *x509.Certificate
	fingerprint string
}

func resolveSafeFile(base, directory, filename string) (string, []byte, error) {
	path := filepath.Join(directory, filename)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	prefix := base + string(filepath.Separator)
	if !strings.HasPrefix(resolved, prefix) && resolved != base {
		return "", nil, os.ErrPermission
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCertificateFileSize {
		return "", nil, os.ErrInvalid
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", nil, err
	}
	return path, data, nil
}

// CertificateInventory summarises the certificates in directory. It never returns key material.
func CertificateInventory(directory string, now time.Time) []*pb.CertificateFileSummary {
	base, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	var keys []keyCandidate
	var certs []certCandidate

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		isKeyExt := ext == ".key" || ext == ".pem"
		isCertExt := ext == ".crt" || ext == ".cer" || ext == ".pem"

		if !isKeyExt && !isCertExt {
			continue
		}

		path, data, err := resolveSafeFile(base, directory, name)
		if err != nil {
			continue
		}

		if isKeyExt {
			keys = append(keys, keyCandidate{
				filename: name,
				path:     path,
				data:     data,
			})
		}

		if isCertExt {
			// Find first CERTIFICATE PEM block
			rest := data
			for len(rest) > 0 {
				var block *pem.Block
				block, rest = pem.Decode(rest)
				if block == nil {
					break
				}
				if block.Type == "CERTIFICATE" {
					leaf, err := x509.ParseCertificate(block.Bytes)
					if err == nil {
						sum := sha256.Sum256(block.Bytes)
						certs = append(certs, certCandidate{
							filename:    name,
							path:        path,
							data:        data,
							leaf:        leaf,
							fingerprint: hex.EncodeToString(sum[:]),
						})
					}
					break
				}
			}
		}
	}

	var summaries []*pb.CertificateFileSummary

	for _, cert := range certs {
		// Find matching private key
		matchedKey := ""

		baseWithoutExt := strings.TrimSuffix(cert.filename, filepath.Ext(cert.filename))

		// Sort candidate keys with priorities:
		// 1. Same base name with .key
		// 2. certbot privkey.pem
		// 3. same file (if cert.data has private key)
		// 4. same base name with .pem
		// 5. others
		type scoredKey struct {
			key   keyCandidate
			score int
		}
		var scored []scoredKey
		for _, k := range keys {
			score := 100
			if k.filename == baseWithoutExt+".key" {
				score = 1
			} else if (cert.filename == "fullchain.pem" || cert.filename == "cert.pem") && k.filename == "privkey.pem" {
				score = 2
			} else if k.filename == cert.filename {
				score = 3
			} else if k.filename == baseWithoutExt+".pem" {
				score = 4
			}
			scored = append(scored, scoredKey{key: k, score: score})
		}
		sort.SliceStable(scored, func(i, j int) bool {
			if scored[i].score != scored[j].score {
				return scored[i].score < scored[j].score
			}
			return scored[i].key.filename < scored[j].key.filename
		})

		for _, item := range scored {
			if _, err := tls.X509KeyPair(cert.data, item.key.data); err == nil {
				matchedKey = item.key.path
				break
			}
		}

		dnsNames := append([]string{}, cert.leaf.DNSNames...)
		var ipAddrs []string
		for _, ip := range cert.leaf.IPAddresses {
			ipAddrs = append(ipAddrs, ip.String())
		}

		summaries = append(summaries, &pb.CertificateFileSummary{
			CertificatePath:   cert.path,
			KeyPath:           matchedKey,
			Subject:           cert.leaf.Subject.String(),
			DnsNames:          dnsNames,
			IpAddresses:       ipAddrs,
			NotAfter:          timestamppb.New(cert.leaf.NotAfter),
			FingerprintSha256: cert.fingerprint,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CertificatePath < summaries[j].CertificatePath
	})

	if len(summaries) > maxCertificateSummaries {
		summaries = summaries[:maxCertificateSummaries]
	}

	return summaries
}

// InventoryDigest is a stable hash of the summaries, used to decide when to re-report.
func InventoryDigest(files []*pb.CertificateFileSummary) string {
	return inventoryDigest(files)
}

func inventoryDigest(files []*pb.CertificateFileSummary) string {
	entries := make([]string, len(files))
	for i, f := range files {
		var sec int64
		if f.NotAfter != nil {
			sec = f.NotAfter.Seconds
		}
		entries[i] = f.CertificatePath + "|" + f.KeyPath + "|" + f.FingerprintSha256 + "|" + strconv.FormatInt(sec, 10)
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:])
}
