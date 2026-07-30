// +build ignore

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

func main() {
	// Generate CA private key
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	// Create CA certificate template
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"GoVpn CA"},
			CommonName:   "GoVpn Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	// Self-sign the CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		panic(err)
	}

	// Save CA certificate
	caCertFile, _ := os.Create("certs/ca.pem")
	pem.Encode(caCertFile, &pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caCertFile.Close()

	// Save CA private key
	caKeyDER, _ := x509.MarshalECPrivateKey(caKey)
	caKeyFile, _ := os.Create("certs/ca-key.pem")
	pem.Encode(caKeyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})
	caKeyFile.Close()

	fmt.Println("✅ CA certificate generated:")
	fmt.Println("   certs/ca.pem      - CA certificate (install in browser/OS)")
	fmt.Println("   certs/ca-key.pem  - CA private key (keep secret)")
	fmt.Println("")
	fmt.Println("📋 Next steps:")
	fmt.Println("   1. Install certs/ca.pem in Windows Trusted Root CA")
	fmt.Println("   2. Run: go run proxy-mitm.go")
}
