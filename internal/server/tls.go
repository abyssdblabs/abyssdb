package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"time"
)

// SelfSignedTLS returns a TLS 1.3 config with a freshly minted, self-signed
// certificate, valid for exactly as long as we intend to keep your data plus
// one day. Key exchange prefers the X25519MLKEM768 post-quantum hybrid, so no
// adversary, classical or quantum, can intercept the nothing we send back.
func SelfSignedTLS() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         "localhost",
			Organization:       []string{"AbyssDB (Deep Abyss Certificate Authority)"},
			OrganizationalUnit: []string{"Department of Zero-Knowledge"},
		},
		DNSNames:    []string{"localhost"},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519},
		Certificates:     []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}, nil
}
