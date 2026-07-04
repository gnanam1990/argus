package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func getenvMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestRunGracefulShutdown checks run serves /status on an ephemeral loopback
// port and, once ctx is cancelled, shuts down within the bound deadline and
// stops accepting connections (signal.NotifyContext + srv.Shutdown wiring).
func TestRunGracefulShutdown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, "127.0.0.1:0", getenvMap(nil), discardLogger(), ready) }()

	addr := <-ready
	res, err := http.Get("http://" + addr + "/status")
	if err != nil {
		t.Fatalf("get /status: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v, want nil (graceful shutdown)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("graceful shutdown did not complete in time")
	}

	// The listener must actually be closed after shutdown.
	if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		t.Error("server should be unreachable after shutdown")
	}
}

// TestRunRefusesNonLoopbackWithoutToken keeps the pre-existing fail-closed
// rule: this must return an error WITHOUT ever binding a socket (checked
// before net.Listen), so it's safe to exercise with a non-loopback address in
// a hermetic test.
func TestRunRefusesNonLoopbackWithoutToken(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), "0.0.0.0:0", getenvMap(nil), discardLogger(), nil)
	if err == nil {
		t.Fatal("expected an error for a non-loopback bind without a token")
	}
	if !strings.Contains(err.Error(), "ARGUS_GUEST_TOKEN") {
		t.Errorf("error = %v, want mention of ARGUS_GUEST_TOKEN", err)
	}
}

// TestRunTLSServesHTTPS checks the ARGUS_GUEST_TLS_CERT/KEY opt-in actually
// serves HTTPS end to end, using a freshly generated self-signed cert.
func TestRunTLSServesHTTPS(t *testing.T) {
	t.Parallel()
	certPath, keyPath := generateSelfSignedCert(t, t.TempDir())
	getenv := getenvMap(map[string]string{
		"ARGUS_GUEST_TLS_CERT": certPath,
		"ARGUS_GUEST_TLS_KEY":  keyPath,
	})

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, "127.0.0.1:0", getenv, discardLogger(), ready) }()

	addr := <-ready
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // test-only self-signed cert
	res, err := client.Get("https://" + addr + "/status")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete in time")
	}
}

// TestWarnIfCleartext exercises the cleartext-warning decision directly
// (never binds a socket, so a non-loopback case is safe to test here).
func TestWarnIfCleartext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
		tls  bool
		want bool
	}{
		{"loopback plaintext", "127.0.0.1:7180", false, false},
		{"loopback tls", "127.0.0.1:7180", true, false},
		{"non-loopback tls", "0.0.0.0:7180", true, false},
		{"non-loopback plaintext", "0.0.0.0:7180", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder
			log := slog.New(slog.NewTextHandler(&buf, nil))
			warnIfCleartext(log, c.addr, c.tls)
			got := strings.Contains(buf.String(), "PLAINTEXT")
			if got != c.want {
				t.Errorf("warning logged = %v, want %v (log=%q)", got, c.want, buf.String())
			}
		})
	}
}

// generateSelfSignedCert writes a fresh self-signed loopback cert/key pair
// under dir, for exercising ListenAndServeTLS-equivalent code paths without a
// real CA or network dependency.
func generateSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatal(err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatal(err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
