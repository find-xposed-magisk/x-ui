// Package proxyclient builds HTTP clients that reach the internet through a
// user-supplied proxy. The panel needs it where the host itself has no direct
// route to a service — most often a server inside a censored network that can
// only reach Telegram through one of the panel's own tunnels.
package proxyclient

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alireza0/x-ui/util/common"
)

// Schemes lists the proxy URL schemes New accepts, for use in error messages
// and UI hints.
var Schemes = []string{"http", "https", "socks5", "socks5h"}

// Only the connection setup is bounded here. A blanket http.Client.Timeout
// would also cap the body transfer, which breaks both long-polling and large
// uploads; callers bound whole calls with a context instead.
const (
	dialTimeout = 10 * time.Second
	tlsTimeout  = 10 * time.Second
)

// Parse validates a proxy URL without building a client, so a bad value can be
// rejected when the user saves it rather than when the bot next starts.
func Parse(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, common.NewErrorf("proxy url <%v> is not a valid url: %v", trimmed, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return nil, common.NewErrorf("proxy url <%v> needs a scheme, one of %v", trimmed, strings.Join(Schemes, ", "))
	}
	supported := false
	for _, candidate := range Schemes {
		if scheme == candidate {
			supported = true
			break
		}
	}
	if !supported {
		return nil, common.NewErrorf("proxy scheme <%v> is not supported, use one of %v", parsed.Scheme, strings.Join(Schemes, ", "))
	}
	if parsed.Host == "" {
		return nil, common.NewErrorf("proxy url <%v> is missing a host", trimmed)
	}
	if parsed.Port() == "" {
		return nil, common.NewErrorf("proxy url <%v> is missing a port", trimmed)
	}
	parsed.Scheme = scheme
	return parsed, nil
}

// New returns an HTTP client routed through rawURL. When rawURL is empty the
// client falls back to the standard HTTP_PROXY/HTTPS_PROXY/NO_PROXY environment
// variables, which is what a bare http.Client would have done, so an admin who
// configured the proxy through the service unit keeps working.
//
// The returned client has no overall deadline: callers pass a context sized to
// what they are doing, because a long-poll and a database upload need very
// different ones.
func New(rawURL string) (*http.Client, error) {
	parsed, err := Parse(rawURL)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   tlsTimeout,
		ExpectContinueTimeout: time.Second,
	}
	if parsed != nil {
		// net/http dials http, https, socks5 and socks5h proxies itself, including
		// username/password authentication, so no extra dialer is needed.
		transport.Proxy = http.ProxyURL(parsed)
	}

	return &http.Client{Transport: transport}, nil
}
