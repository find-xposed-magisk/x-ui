package network

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net"
	"os"
	"strings"
	"time"

	"github.com/alireza0/x-ui/util/common"
	utls "github.com/refraction-networking/utls"
)

func NewTLSConfig(cert tls.Certificate, domain string) *tls.Config {
	if domain == "" {
		return &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	crt := cert
	return &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if !strings.EqualFold(hello.ServerName, domain) {
				return nil, common.NewErrorf("tls: unrecognized server name %q", hello.ServerName)
			}
			return &crt, nil
		},
	}
}

func GetCertHash(certFile string, certContent string) ([]string, error) {
	var certBytes []byte
	if path := strings.TrimSpace(certFile); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		certBytes = b
	} else if strings.TrimSpace(certContent) != "" {
		certBytes = []byte(certContent)
	} else {
		return nil, common.NewError("no certificate provided")
	}

	var certs []*x509.Certificate
	if bytes.Contains(certBytes, []byte("BEGIN")) {
		rest := certBytes
		for {
			block, remain := pem.Decode(rest)
			if block == nil {
				break
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, common.NewError("unable to decode certificate: ", err)
			}
			certs = append(certs, cert)
			rest = remain
		}
	} else {
		parsed, err := x509.ParseCertificates(certBytes)
		if err != nil {
			return nil, common.NewError("unable to parse certificates: ", err)
		}
		certs = parsed
	}

	if len(certs) == 0 {
		return nil, common.NewError("no certificates found")
	}

	hashes := make([]string, 0, len(certs))
	for _, cert := range certs {
		sum := sha256.Sum256(cert.Raw)
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	return hashes, nil
}

func GetTlsPing(domain string, port string) (any, error) {
	if domain == "" {
		return "", common.NewError("domain is empty")
	}
	if port == "" {
		port = "443"
	}

	d := net.Dialer{Timeout: 10 * time.Second}
	tcpConn, err := d.Dial("tcp", domain+":"+port)
	if err != nil {
		return "", common.NewErrorf("Failed to dial tcp: %s", err)
	}
	tlsConn := utls.UClient(tcpConn, &utls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2", "http/1.1"},
	}, utls.HelloChrome_Auto)
	err = tlsConn.Handshake()
	if err != nil {
		return "", common.NewErrorf("Failed to handshake: %s", err)
	}
	var leaf *x509.Certificate
	for _, cert := range tlsConn.ConnectionState().PeerCertificates {
		if len(cert.DNSNames) != 0 {
			leaf = cert
			break
		}
	}
	leafHash := sha256.Sum256(leaf.Raw)
	leafObj := map[string]string{
		"leafHash": hex.EncodeToString(leafHash[:]),
	}

	return leafObj, nil

}
