package proxyclient

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestParseAcceptsSupportedSchemes(t *testing.T) {
	tests := []string{
		"socks5://127.0.0.1:1080",
		"socks5h://127.0.0.1:1080",
		"socks5://user:pass@127.0.0.1:1080",
		"http://127.0.0.1:8080",
		"https://proxy.example.com:8443",
		"SOCKS5://127.0.0.1:1080", // the scheme is normalised, not rejected
		"  http://127.0.0.1:8080  ",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			parsed, err := Parse(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed == nil {
				t.Fatal("a non-empty url parsed to nil")
			}
			if parsed.Scheme != strings.ToLower(parsed.Scheme) {
				t.Fatalf("scheme %q was not normalised to lower case", parsed.Scheme)
			}
		})
	}
}

func TestParseTreatsBlankAsDirect(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t"} {
		parsed, err := Parse(raw)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", raw, err)
		}
		if parsed != nil {
			t.Fatalf("%q should mean a direct connection, got %v", raw, parsed)
		}
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"no scheme", "127.0.0.1:1080"},
		{"unsupported scheme", "ftp://127.0.0.1:21"},
		{"socks4 is not supported", "socks4://127.0.0.1:1080"},
		{"no host", "socks5://"},
		{"no port", "socks5://127.0.0.1"},
		{"not a url", "://:"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.raw); err == nil {
				t.Fatalf("expected %q to be rejected", test.raw)
			}
		})
	}
}

// The error text is shown to the admin in the settings page, so it has to name
// the offending value and the supported schemes rather than just say "invalid".
func TestParseErrorIsActionable(t *testing.T) {
	_, err := Parse("ftp://127.0.0.1:21")
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "ftp") {
		t.Errorf("error %q does not name the rejected scheme", message)
	}
	if !strings.Contains(message, "socks5") {
		t.Errorf("error %q does not list the supported schemes", message)
	}
}

func TestNewWithoutProxyStillReturnsAClient(t *testing.T) {
	client, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a usable client for a direct connection")
	}
	// The client deliberately carries no overall deadline: a blanket timeout
	// would cap long-polling and large uploads alike. Connection setup is still
	// bounded by the transport.
	if client.Timeout != 0 {
		t.Fatalf("client timeout = %v, want none so callers can size it per call", client.Timeout)
	}
	if client.Transport == nil {
		t.Fatal("the direct client has no transport, so connection setup is unbounded")
	}
}

// A bare http.Client honours HTTP_PROXY and friends. The panel used to get that
// for free, so an admin who set the proxy in the service unit rather than in the
// settings page must keep working.
//
// This is asserted structurally rather than with a live request: net/http reads
// the environment through a sync.Once, so whichever test ran first would fix the
// value for the whole binary, and it never proxies loopback addresses anyway.
func TestNewFallsBackToEnvironmentProxy(t *testing.T) {
	client, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("transport has no proxy function, so HTTP_PROXY is ignored")
	}
	want := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if got := reflect.ValueOf(transport.Proxy).Pointer(); got != want {
		t.Fatal("the direct client does not consult the environment for a proxy")
	}
}

// An explicit setting is the admin's most specific instruction, so it has to win
// over whatever the environment says.
func TestConfiguredProxyBeatsEnvironment(t *testing.T) {
	var configuredUsed int
	var mu sync.Mutex

	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		configuredUsed++
		mu.Unlock()
		fmt.Fprint(w, "configured")
	}))
	defer configured.Close()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("NO_PROXY", "")

	client, err := New(configured.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A non-loopback target, so the environment function would apply if it were
	// still installed; the request must instead reach the configured proxy.
	resp, err := client.Get("http://example.invalid/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if configuredUsed != 1 {
		t.Fatalf("the configured proxy saw %d requests, want 1", configuredUsed)
	}
}

func TestNewRejectsBadProxy(t *testing.T) {
	if _, err := New("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected New to reject an unsupported scheme")
	}
}

func TestNewRoutesThroughHTTPProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "from origin")
	}))
	defer origin.Close()

	var proxied int
	var mu sync.Mutex
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proxied++
		mu.Unlock()
		// A forward proxy receives the absolute URL; answer it directly.
		fmt.Fprint(w, "from proxy")
	}))
	defer proxyServer.Close()

	client, err := New(proxyServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	mu.Lock()
	seen := proxied
	mu.Unlock()
	if seen != 1 {
		t.Fatalf("the proxy saw %d requests, want 1", seen)
	}
	if string(body) != "from proxy" {
		t.Fatalf("body = %q, want the proxy's response", body)
	}
}

// net/http dials SOCKS5 itself; this proves the panel's settings reach that path
// rather than silently falling back to a direct connection.
func TestNewRoutesThroughSocks5Proxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "through socks")
	}))
	defer origin.Close()

	socks, dialed := newSocks5Server(t)
	defer socks.Close()

	client, err := New("socks5://" + socks.Addr().String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("request through socks5 failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if string(body) != "through socks" {
		t.Fatalf("body = %q", body)
	}
	if got := dialed(); got != 1 {
		t.Fatalf("the socks5 server handled %d connections, want 1", got)
	}
}

// newSocks5Server starts a minimal SOCKS5 CONNECT proxy: enough of RFC 1928 to
// prove the client really dials through it rather than connecting directly.
func newSocks5Server(t *testing.T) (net.Listener, func() int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var mu sync.Mutex
	count := 0

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			count++
			mu.Unlock()
			go serveSocks5(conn)
		}
	}()

	return listener, func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

func serveSocks5(client net.Conn) {
	defer client.Close()

	// Greeting: version, method count, methods.
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		return
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	// No authentication required.
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Request: version, command, reserved, address type.
	request := make([]byte, 4)
	if _, err := io.ReadFull(client, request); err != nil {
		return
	}
	var host string
	switch request[3] {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(client, addr); err != nil {
			return
		}
		host = net.IP(addr).String()
	case 0x03: // domain name
		length := make([]byte, 1)
		if _, err := io.ReadFull(client, length); err != nil {
			return
		}
		name := make([]byte, length[0])
		if _, err := io.ReadFull(client, name); err != nil {
			return
		}
		host = string(name)
	case 0x04: // IPv6
		addr := make([]byte, 16)
		if _, err := io.ReadFull(client, addr); err != nil {
			return
		}
		host = net.IP(addr).String()
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		return
	}
	target := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(portBytes)))

	upstream, err := net.Dial("tcp", target)
	if err != nil {
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	// Success, with a dummy bound address.
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		io.Copy(upstream, client)
		done <- struct{}{}
	}()
	io.Copy(client, upstream)
	<-done
}
