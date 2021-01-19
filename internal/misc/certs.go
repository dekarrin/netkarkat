package misc

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
)

// LoadCertificates loads certificates from the given filename and returns them.
// It supports both PEM-files and DER files.
func LoadCertificates(filename string) (certs []*x509.Certificate, err error) {
	certBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("could not read certificates: %v", err)
	}

	// do an initial decode to find if the file is PEM-based or not
	block, _ := pem.Decode(certBytes)

	if block == nil {
		// reading PEM failed; try just decoding it as DER
		return x509.ParseCertificates(certBytes)
	}

	entryNum := 0
	for block, certBytes := pem.Decode(certBytes); block != nil; block, certBytes = pem.Decode(certBytes) {
		entryNum++
		if block.Type != "CERTIFICATE" {
			continue
		}
		parsedCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("problem reading entry #%d from PEM file: %v", entryNum, err)
		}
		certs = append(certs, parsedCert)
	}

	if len(certs) < 1 {
		return nil, fmt.Errorf("no certificates found")
	}

	return certs, nil
}
