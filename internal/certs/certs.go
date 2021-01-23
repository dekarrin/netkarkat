package certs

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// GenerateSelfSignedTLSServerCertificate creates a CA and an X509 keypair
// and uses the CA to sign the keypair. The generated CA will last for only 2 days, as will the generated cert.
//
// Note that most clients will not trust the certificate unless
// explicitly told to.
//
// If cn is provided, it will be the common name of the certificate (default is to use localhost
// as the common name.
// If ips is provided, it will be added to the list of IPAddresses that the cert is for. Default
// is loopback for both ipv6 and ipv4; if any ips are provided by caller, those defaults will be
// entirely replaced by the provided ones.
func GenerateSelfSignedTLSServerCertificate(cn string, ips []net.IP) (cert tls.Certificate, caPEM []byte, err error) {
	ca, caBytes, caKey, err := generateCertificateAuthority()
	if err != nil {
		return cert, caPEM, fmt.Errorf("could not generate CA: %v", err)
	}

	_, x509CertBytes, x509CertKey, err := generateSignedCertificate(ca, caKey, cn, ips)
	if err != nil {
		return cert, caPEM, fmt.Errorf("could not generate signed cert: %v", err)
	}

	cert, err = tls.X509KeyPair(encodeAsPEM(x509CertBytes, x509CertKey))
	if err != nil {
		return cert, caPEM, fmt.Errorf("could not put certs in TLS-ready keypair: %v", err)
	}

	caPEM, _ = encodeAsPEM(caBytes, caKey)
	return cert, caPEM, nil
}

func generateSignedCertificate(ca *x509.Certificate, caKey *rsa.PrivateKey, cn string, ips []net.IP) (cert *x509.Certificate, signedCert []byte, key *rsa.PrivateKey, err error) {
	cert = &x509.Certificate{
		SerialNumber: big.NewInt(413 * 612 * 1111 * 1125),
		Subject: pkix.Name{
			CommonName:         "localhost",
			OrganizationalUnit: []string{"Generated CAs"},
			Organization:       []string{"Netkarkat"},
			Country:            []string{"US"},
			Province:           []string{"MN"},
			Locality:           []string{"Minneapolis"},
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(0, 0, 2),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	if cn != "" {
		cert.Subject.CommonName = cn
	}
	if len(ips) > 0 {
		cert.IPAddresses = []net.IP{}

		// iterate instead of assigning to ensure that caller doesn't later modify the slice
		for _, ip := range ips {
			cert.IPAddresses = append(cert.IPAddresses, ip)
		}
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 8192)
	if err != nil {
		return nil, nil, nil, err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &privKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	return cert, certBytes, privKey, nil
}

func generateCertificateAuthority() (certificateAuthority *x509.Certificate, signedCa []byte, key *rsa.PrivateKey, err error) {
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2021),
		Subject: pkix.Name{
			CommonName:         "Netkk-generated Certificate Authority",
			OrganizationalUnit: []string{"Generated CAs"},
			Organization:       []string{"Netkarkat"},
			Country:            []string{"US"},
			Province:           []string{"MN"},
			Locality:           []string{"Minneapolis"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 2),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 8192)
	if err != nil {
		return nil, nil, nil, err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// we could then encode the ca but since we are probably just going to immediately use it
	// to sign a cert and then discard, we can just return the non-encoded and have callers
	// decide whether to save it as pem-encoded.
	return ca, certBytes, privKey, nil
}

func encodeAsPEM(cert []byte, key *rsa.PrivateKey) (pemCert []byte, pemKey []byte) {
	certPemBuf := new(bytes.Buffer)
	pem.Encode(certPemBuf, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	})

	keyPemBuf := new(bytes.Buffer)
	pem.Encode(keyPemBuf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	return certPemBuf.Bytes(), keyPemBuf.Bytes()
}
