package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mitmCACert *x509.Certificate
	mitmCAKey  *ecdsa.PrivateKey
	leafCache  sync.Map // host string -> *tls.Certificate

	// mitmMu — mitmCACert/mitmCAKey/adblock, AdBlock ayarı çalışırken (restart
	// olmadan) açılırsa bir goroutine'den geç (lazy) kurulabilir; bu sırada proxy
	// zaten istek işliyor olabileceğinden okuma/yazma bu mutex ile korunur.
	mitmMu sync.RWMutex
)

// mitmReady — CA ve adblock motorunun ikisi de hazır mı (MITM için ön koşul).
func mitmReady() bool {
	mitmMu.RLock()
	defer mitmMu.RUnlock()
	return mitmCACert != nil && adblock != nil
}

// getCA — mitmCACert/mitmCAKey çiftini kilit altında döner.
func getCA() (*x509.Certificate, *ecdsa.PrivateKey) {
	mitmMu.RLock()
	defer mitmMu.RUnlock()
	return mitmCACert, mitmCAKey
}

// getAdblock — adblock motor referansını kilit altında döner.
func getAdblock() *adblockEngine {
	mitmMu.RLock()
	defer mitmMu.RUnlock()
	return adblock
}

// mitmCAPaths — CA sertifika/anahtar dosyalarının config ile aynı dizindeki yolu.
func mitmCAPaths() (certPath, keyPath string) {
	dir := filepath.Dir(configFilePath())
	return filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
}

// ensureMITMCA — CA sertifikasını diskten yükler; yoksa üretip kaydeder.
// Başarılı olursa mitmCACert/mitmCAKey global değişkenlerini doldurur.
func ensureMITMCA() error {
	certPath, keyPath := mitmCAPaths()
	if cert, key, err := loadCA(certPath, keyPath); err == nil {
		mitmMu.Lock()
		mitmCACert, mitmCAKey = cert, key
		mitmMu.Unlock()
		return nil
	}
	cert, key, err := generateCA()
	if err != nil {
		return fmt.Errorf("CA üretilemedi: %w", err)
	}
	if err := saveCA(certPath, keyPath, cert, key); err != nil {
		return fmt.Errorf("CA kaydedilemedi: %w", err)
	}
	mitmMu.Lock()
	mitmCACert, mitmCAKey = cert, key
	mitmMu.Unlock()
	return nil
}

// generateCA — 10 yıl geçerli, ECDSA P-256 imzalı bir root CA üretir.
func generateCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LunaProxy Root CA", Organization: []string{"LunaProxy"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// saveCA — CA sertifika ve anahtarını PEM formatında diske yazar.
func saveCA(certPath, keyPath string, cert *x509.Certificate, key *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return err
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		return err
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}

// loadCA — diskteki PEM dosyalarından CA sertifika/anahtarını okur.
func loadCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("ca.crt PEM çözülemedi")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("ca.key PEM çözülemedi")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// leafCertFor — host için CA tarafından imzalanmış, 1 yıl geçerli bir
// leaf sertifika üretir/cache'den döner (sync.Map, host -> *tls.Certificate).
func leafCertFor(host string) (*tls.Certificate, error) {
	if v, ok := leafCache.Load(host); ok {
		return v.(*tls.Certificate), nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	caCert, caKey := getCA()
	if caCert == nil {
		return nil, fmt.Errorf("CA henüz hazır değil")
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	leaf := &tls.Certificate{
		Certificate: [][]byte{der, caCert.Raw},
		PrivateKey:  key,
	}
	leafCache.Store(host, leaf)
	return leaf, nil
}

// hostOnly — "example.com:443" gibi bir host:port değerinden sadece
// hostname'i döner; port yoksa değeri olduğu gibi döner.
func hostOnly(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return h
}
