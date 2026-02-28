// Package allowlist implements a lightweight HTTP CONNECT forward proxy that
// restricts outbound traffic to a configurable set of allowed hosts. The proxy
// is designed to run as a sidecar alongside agent sandboxes. Traffic is routed
// through it by setting HTTP_PROXY / HTTPS_PROXY in the sandbox environment.
package allowlist

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Proxy is an HTTP CONNECT forward proxy with host allowlisting.
type Proxy struct {
	allowed   map[string]bool
	mu        sync.RWMutex
	logger    *log.Logger
	listenAddr string
}

// New creates an allowlist proxy. The allowed list is a set of host patterns.
// Patterns support exact match ("api.anthropic.com") and wildcard subdomains
// ("*.anthropic.com"). Ports are stripped before matching.
func New(allowed []string, listenAddr string, logger *log.Logger) *Proxy {
	m := make(map[string]bool, len(allowed))
	for _, h := range allowed {
		m[strings.ToLower(h)] = true
	}
	return &Proxy{
		allowed:    m,
		logger:     logger,
		listenAddr: listenAddr,
	}
}

// UpdateAllowlist replaces the allowed hosts at runtime.
func (p *Proxy) UpdateAllowlist(hosts []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[strings.ToLower(h)] = true
	}
	p.allowed = m
}

// isAllowed checks a host:port against the allowlist.
func (p *Proxy) isAllowed(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		// No port present, use as-is.
		host = hostport
	}
	host = strings.ToLower(host)

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Exact match.
	if p.allowed[host] {
		return true
	}

	// Wildcard match: *.example.com matches foo.example.com, bar.baz.example.com.
	parts := strings.SplitN(host, ".", 2)
	for len(parts) == 2 {
		wildcard := "*." + parts[1]
		if p.allowed[wildcard] {
			return true
		}
		parts = strings.SplitN(parts[1], ".", 2)
	}

	return false
}

// ListenAndServe starts the allowlist proxy.
func (p *Proxy) ListenAndServe() error {
	srv := &http.Server{
		Addr:         p.listenAddr,
		Handler:      p,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	p.logger.Printf("allowlist proxy listening on %s (%d hosts allowed)", p.listenAddr, len(p.allowed))
	return srv.ListenAndServe()
}

// ServeHTTP handles both CONNECT tunnels (HTTPS) and plain HTTP requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

// handleConnect processes HTTPS CONNECT tunnels.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !p.isAllowed(r.Host) {
		p.logger.Printf("BLOCKED CONNECT %s", r.Host)
		http.Error(w, fmt.Sprintf("host %q not in allowlist", r.Host), http.StatusForbidden)
		return
	}

	p.logger.Printf("CONNECT %s", r.Host)

	targetConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		targetConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	// Send 200 OK to establish the tunnel.
	w.WriteHeader(http.StatusOK)

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		targetConn.Close()
		return
	}

	// Bidirectional copy.
	go transfer(targetConn, clientConn)
	go transfer(clientConn, targetConn)
}

// handleHTTP proxies plain HTTP requests.
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	if !p.isAllowed(host) {
		p.logger.Printf("BLOCKED %s %s", r.Method, r.URL)
		http.Error(w, fmt.Sprintf("host %q not in allowlist", host), http.StatusForbidden)
		return
	}

	p.logger.Printf("%s %s", r.Method, r.URL)

	// Build the upstream request.
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers, stripping hop-by-hop headers.
	copyHeaders(outReq.Header, r.Header)

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// transfer copies data between two connections and closes both when done.
func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src) //nolint:errcheck
}

// copyHeaders copies HTTP headers, skipping hop-by-hop headers.
func copyHeaders(dst, src http.Header) {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}
	for k, vv := range src {
		if hopByHop[k] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
