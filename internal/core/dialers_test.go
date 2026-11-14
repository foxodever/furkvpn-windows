package core

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

// Xray files gRPC and XHTTP connections in tables of the process and leaves them there when the
// instance closes; closeInstance must take them out and close them. This is also the test that
// notices when an update of Xray moves what dialers.go reaches into.
func TestClosedInstanceLeavesNoConnections(t *testing.T) {
	const id = "6b1e7a3c-0d64-4c7e-9f00-000000000001"
	certPEM, keyPEM, pin := selfSignedCert(t, "x.furk.test")
	grpcPort, xhttpPort := freeTCPPort(t), freeTCPPort(t)
	lines := func(b []byte) string {
		out, _ := json.Marshal(strings.Split(strings.TrimSpace(string(b)), "\n"))
		return string(out)
	}
	server, err := startTestInstance(mustParse(t, fmt.Sprintf(`{
	  "log": {"loglevel": "none"},
	  "inbounds": [
	    {"protocol": "vless", "listen": "127.0.0.1", "port": %d, "settings": {"clients": [{"id": %q}], "decryption": "none"},
	      "streamSettings": {"network": "grpc", "grpcSettings": {"serviceName": "svc"}}},
	    {"protocol": "vless", "listen": "127.0.0.1", "port": %d, "settings": {"clients": [{"id": %q}], "decryption": "none"},
	      "streamSettings": {"network": "xhttp", "xhttpSettings": {"path": "/x"},
	        "security": "tls", "tlsSettings": {"alpn": ["h2"], "certificates": [{"certificate": %s, "key": %s}]}}}],
	  "outbounds": [{"protocol": "freedom", "settings": {"finalRules": [{"action": "allow"}]}}]}`, grpcPort, id, xhttpPort, id, lines(certPEM), lines(keyPEM))))
	if err != nil {
		t.Fatal(err)
	}
	defer closeInstance(server)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer site.Close()

	user := fmt.Sprintf(`"settings": {"vnext": [{"address": "127.0.0.1", "port": %%d, "users": [{"id": %q, "encryption": "none"}]}]}`, id)
	client, err := startTestInstance(mustParse(t, fmt.Sprintf(`{
	  "log": {"loglevel": "none"},
	  "outbounds": [
	    {"tag": "grpc", "protocol": "vless", `+user+`,
	      "streamSettings": {"network": "grpc", "grpcSettings": {"serviceName": "svc"}}},
	    {"tag": "xhttp", "protocol": "vless", `+user+`,
	      "streamSettings": {"network": "xhttp", "xhttpSettings": {"path": "/x"},
	        "security": "tls", "tlsSettings": {"serverName": "x.furk.test", "alpn": ["h2"], "pinnedPeerCertSha256": %q}}}]}`,
		grpcPort, xhttpPort, pin)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"grpc", "xhttp"} {
		if ms, err := httpDelay(client, tag, site.URL, 3*time.Second); err != nil || ms < 0 {
			closeInstance(client)
			t.Fatalf("through %s: %d, %v", tag, ms, err)
		}
	}
	filed := func() (grpc, xhttp int) {
		grpcDialersLock.Lock()
		grpc = len(grpcDialers)
		grpcDialersLock.Unlock()
		xhttpDialersLock.Lock()
		xhttp = len(xhttpDialers)
		xhttpDialersLock.Unlock()
		return
	}
	// Both are there while the instance lives: these are Xray's tables, not variables of our own.
	if g, x := filed(); g != 1 || x != 1 {
		closeInstance(client)
		t.Fatalf("filed before closing: gRPC %d, XHTTP %d, want 1 and 1", g, x)
	}
	if n := len(streamSettingsOf(client)); n != 2 {
		t.Errorf("stream settings of %d outbounds found, want 2", n)
	}
	closeInstance(client)
	if g, x := filed(); g != 0 || x != 0 {
		t.Fatalf("left after closing: gRPC %d, XHTTP %d", g, x)
	}
	// The connections themselves are closed: nothing reads from them any more.
	readers := []string{"transport.(*http2Client).reader", "http2.(*ClientConn).readLoop"}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var stacks bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 1)
		left := ""
		for _, r := range readers {
			if strings.Contains(stacks.String(), r) {
				left = r
			}
		}
		if left == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a connection of the closed instance is still read (%s)", left)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func selfSignedCert(t *testing.T, name string) (certPEM, keyPEM []byte, sha string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		hex.EncodeToString(sum[:])
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
