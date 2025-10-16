package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/spf13/cobra"
)

var (
	tlsOutDir     string
	tlsHosts      string
	tlsDays       int
	tlsServerCN   string
	tlsClientCN   string
	tlsRSAKeyBits int
)

var tlsCmd = &cobra.Command{
	Use:   "tls",
	Short: "TLS utilities",
}

var tlsGenCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate CA, server, and client certificates",
	Run: func(cmd *cobra.Command, args []string) {
		if err := os.MkdirAll(tlsOutDir, 0700); err != nil {
			logx.Error("TLS", fmt.Sprintf("failed to create output dir: %v", err))
			return
		}

		notBefore := time.Now().Add(-time.Hour)
		notAfter := notBefore.Add(time.Duration(tlsDays) * 24 * time.Hour)

		// CA
		caKey, err := rsa.GenerateKey(rand.Reader, tlsRSAKeyBits)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("generate CA key: %v", err))
			return
		}
		caSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		caTmpl := &x509.Certificate{
			SerialNumber:          caSerial,
			Subject:               pkix.Name{CommonName: "mmn-ca"},
			NotBefore:             notBefore,
			NotAfter:              notAfter,
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
			IsCA:                  true,
			BasicConstraintsValid: true,
			MaxPathLenZero:        true,
		}
		caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("create CA cert: %v", err))
			return
		}
		if err := writeKeyPair(filepath.Join(tlsOutDir, "ca.crt"), filepath.Join(tlsOutDir, "ca.key"), caDER, caKey); err != nil {
			logx.Error("TLS", fmt.Sprintf("write CA: %v", err))
			return
		}

		// Server
		serverKey, err := rsa.GenerateKey(rand.Reader, tlsRSAKeyBits)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("generate server key: %v", err))
			return
		}
		srvSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		srvTmpl := &x509.Certificate{
			SerialNumber:          srvSerial,
			Subject:               pkix.Name{CommonName: tlsServerCN},
			NotBefore:             notBefore,
			NotAfter:              notAfter,
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
		}
		addHostsToCert(srvTmpl, tlsHosts)
		caCert, _ := x509.ParseCertificate(caDER)
		srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &serverKey.PublicKey, caKey)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("create server cert: %v", err))
			return
		}
		if err := writeKeyPair(filepath.Join(tlsOutDir, "server.crt"), filepath.Join(tlsOutDir, "server.key"), srvDER, serverKey); err != nil {
			logx.Error("TLS", fmt.Sprintf("write server: %v", err))
			return
		}

		// Client
		clientKey, err := rsa.GenerateKey(rand.Reader, tlsRSAKeyBits)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("generate client key: %v", err))
			return
		}
		cliSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		cliTmpl := &x509.Certificate{
			SerialNumber:          cliSerial,
			Subject:               pkix.Name{CommonName: tlsClientCN},
			NotBefore:             notBefore,
			NotAfter:              notAfter,
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
		}
		cliDER, err := x509.CreateCertificate(rand.Reader, cliTmpl, caCert, &clientKey.PublicKey, caKey)
		if err != nil {
			logx.Error("TLS", fmt.Sprintf("create client cert: %v", err))
			return
		}
		if err := writeKeyPair(filepath.Join(tlsOutDir, "client.crt"), filepath.Join(tlsOutDir, "client.key"), cliDER, clientKey); err != nil {
			logx.Error("TLS", fmt.Sprintf("write client: %v", err))
			return
		}

		logx.Info("TLS", fmt.Sprintf("Certificates written to %s (ca.crt/ca.key, server.crt/server.key, client.crt/client.key)", tlsOutDir))
	},
}

func init() {
	rootCmd.AddCommand(tlsCmd)
	tlsCmd.AddCommand(tlsGenCmd)
	tlsGenCmd.Flags().StringVar(&tlsOutDir, "data-dir", "./tls", "Output directory")
	tlsGenCmd.Flags().StringVar(&tlsHosts, "hosts", "localhost,127.0.0.1", "Comma-separated DNS or IP SANs for server cert")
	tlsGenCmd.Flags().IntVar(&tlsDays, "days", 365, "Validity days for certificates")
	tlsGenCmd.Flags().StringVar(&tlsServerCN, "server-cn", "mmn-grpc", "Server certificate")
	tlsGenCmd.Flags().StringVar(&tlsClientCN, "client-cn", "mmn-client", "Client certificate")
	tlsGenCmd.Flags().IntVar(&tlsRSAKeyBits, "rsa-bits", 2048, "RSA key size in bits")
}

func writeKeyPair(certPath string, keyPath string, certDER []byte, key *rsa.PrivateKey) error {
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		certOut.Close()
		return err
	}
	certOut.Close()

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		keyOut.Close()
		return err
	}
	keyOut.Close()
	if err := os.Chmod(keyPath, 0600); err != nil {
		return err
	}
	return nil
}

func addHostsToCert(t *x509.Certificate, hostsCSV string) {
	if hostsCSV == "" {
		return
	}
	parts := strings.Split(hostsCSV, ",")
	for _, h := range parts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			t.IPAddresses = append(t.IPAddresses, ip)
		} else {
			t.DNSNames = append(t.DNSNames, h)
		}
	}
}
