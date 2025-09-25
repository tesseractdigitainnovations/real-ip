package traefik_real_ip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Header constants for IP detection
const (
	XRealIP           = "X-Real-Ip"
	XForwardedFor     = "X-Forwarded-For"
	XForwardedProto   = "X-Forwarded-Proto"
	CFConnectingIP    = "Cf-Connecting-Ip"
	CFIPCountry       = "Cf-Ipcountry"
	TrueClientIP      = "True-Client-Ip"
	XClientIP         = "X-Client-Ip"
	XClusterClientIP  = "X-Cluster-Client-Ip"
	XOriginalFor      = "X-Original-Forwarded-For"
)

// Config holds the plugin configuration
type Config struct {
	// Networks to exclude from real IP detection (trusted proxies)
	ExcludedNets []string `json:"excludednets,omitempty" toml:"excludednets,omitempty" yaml:"excludednets,omitempty"`
	
	// Order of headers to check for real IP (first match wins)
	HeaderOrder []string `json:"headerorder,omitempty" toml:"headerorder,omitempty" yaml:"headerorder,omitempty"`
	
	// Whether to trust CloudFlare headers
	TrustCloudFlare bool `json:"trustcloudflare,omitempty" toml:"trustcloudflare,omitempty" yaml:"trustcloudflare,omitempty"`
	
	// Whether to overwrite existing X-Real-IP header
	OverwriteXRealIP bool `json:"overwritexrealip,omitempty" toml:"overwritexrealip,omitempty" yaml:"overwritexrealip,omitempty"`
	
	// Whether to add X-Forwarded-Proto header
	AddForwardedProto bool `json:"addforwardedproto,omitempty" toml:"addforwardedproto,omitempty" yaml:"addforwardedproto,omitempty"`
	
	// Custom header to set with the real IP
	CustomHeader string `json:"customheader,omitempty" toml:"customheader,omitempty" yaml:"customheader,omitempty"`
	
	// Enable debug logging
	Debug bool `json:"debug,omitempty" toml:"debug,omitempty" yaml:"debug,omitempty"`
	
	// Max iterations when parsing X-Forwarded-For chain
	MaxIterations int `json:"maxiterations,omitempty" toml:"maxiterations,omitempty" yaml:"maxiterations,omitempty"`
}

// CreateConfig creates the default plugin configuration
func CreateConfig() *Config {
	return &Config{
		ExcludedNets: []string{
			"10.0.0.0/8",     // Private networks
			"172.16.0.0/12",  // Private networks
			"192.168.0.0/16", // Private networks
			"127.0.0.0/8",    // Loopback
			"::1/128",        // IPv6 loopback
			"fc00::/7",       // IPv6 private
		},
		HeaderOrder: []string{
			CFConnectingIP,
			TrueClientIP,
			XClientIP,
			XClusterClientIP,
			XForwardedFor,
			XOriginalFor,
		},
		TrustCloudFlare:   true,
		OverwriteXRealIP:  true,
		AddForwardedProto: false,
		CustomHeader:      "",
		Debug:             false,
		MaxIterations:     10,
	}
}

// RealIPOverWriter is the main plugin struct
type RealIPOverWriter struct {
	next            http.Handler
	name            string
	excludedNets    []*net.IPNet
	headerOrder     []string
	trustCloudFlare bool
	overwriteXRealIP bool
	addForwardedProto bool
	customHeader    string
	debug           bool
	maxIterations   int
}

// New creates a new RealIP plugin instance
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	// Validate max iterations
	if config.MaxIterations <= 0 {
		config.MaxIterations = 10
	}
	
	plugin := &RealIPOverWriter{
		next:              next,
		name:              name,
		headerOrder:       config.HeaderOrder,
		trustCloudFlare:   config.TrustCloudFlare,
		overwriteXRealIP:  config.OverwriteXRealIP,
		addForwardedProto: config.AddForwardedProto,
		customHeader:      config.CustomHeader,
		debug:             config.Debug,
		maxIterations:     config.MaxIterations,
	}

	// Parse excluded networks
	for _, netStr := range config.ExcludedNets {
		_, network, err := net.ParseCIDR(netStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR network '%s': %w", netStr, err)
		}
		plugin.excludedNets = append(plugin.excludedNets, network)
	}

	if plugin.debug {
		fmt.Printf("[%s] RealIP plugin initialized with %d excluded networks\n", 
			plugin.name, len(plugin.excludedNets))
	}

	return plugin, nil
}

// ServeHTTP processes the HTTP request and determines the real IP
func (r *RealIPOverWriter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	originalIP := r.getRemoteIP(req)
	realIP := r.determineRealIP(req)
	
	if r.debug {
		fmt.Printf("[%s] Original IP: %s, Determined Real IP: %s\n", 
			r.name, originalIP, realIP)
	}

	// Set headers
	r.setIPHeaders(req, realIP)
	
	// Set custom header if specified
	if r.customHeader != "" {
		req.Header.Set(r.customHeader, realIP)
	}
	
	// Add X-Forwarded-Proto if enabled
	if r.addForwardedProto {
		r.setForwardedProto(req)
	}

	r.next.ServeHTTP(rw, req)
}

// determineRealIP finds the real client IP using various methods
func (r *RealIPOverWriter) determineRealIP(req *http.Request) string {
	// Try headers in configured order
	for _, header := range r.headerOrder {
		if ip := r.getIPFromHeader(req, header); ip != "" {
			return ip
		}
	}
	
	// Fallback to remote address
	return r.getRemoteIP(req)
}

// getIPFromHeader extracts IP from a specific header
func (r *RealIPOverWriter) getIPFromHeader(req *http.Request, headerName string) string {
	headerValue := req.Header.Get(headerName)
	if headerValue == "" {
		return ""
	}
	
	switch headerName {
	case XForwardedFor, XOriginalFor:
		return r.parseForwardedFor(headerValue)
	case CFConnectingIP:
		if r.trustCloudFlare {
			return r.validateIP(headerValue)
		}
		return ""
	default:
		return r.validateIP(headerValue)
	}
}

// parseForwardedFor parses X-Forwarded-For header chain
func (r *RealIPOverWriter) parseForwardedFor(headerValue string) string {
	ips := strings.Split(headerValue, ",")
	iterations := 0
	
	// Start from the rightmost IP (most recent proxy)
	for i := len(ips) - 1; i >= 0 && iterations < r.maxIterations; i-- {
		ip := strings.TrimSpace(ips[i])
		if r.isValidPublicIP(ip) {
			return ip
		}
		iterations++
	}
	
	// If no public IP found, return the leftmost (original client)
	if len(ips) > 0 {
		return strings.TrimSpace(ips[0])
	}
	
	return ""
}

// validateIP validates and returns a clean IP address
func (r *RealIPOverWriter) validateIP(ipStr string) string {
	ip := strings.TrimSpace(ipStr)
	
	// Handle IPv6 brackets
	if strings.HasPrefix(ip, "[") && strings.HasSuffix(ip, "]") {
		ip = ip[1 : len(ip)-1]
	}
	
	// Remove port if present
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	
	if net.ParseIP(ip) != nil {
		return ip
	}
	
	return ""
}

// isValidPublicIP checks if an IP is valid and not in excluded networks
func (r *RealIPOverWriter) isValidPublicIP(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	
	// Check against excluded networks
	for _, network := range r.excludedNets {
		if network.Contains(ip) {
			return false
		}
	}
	
	return true
}

// getRemoteIP extracts IP from RemoteAddr
func (r *RealIPOverWriter) getRemoteIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// setIPHeaders sets the appropriate IP headers
func (r *RealIPOverWriter) setIPHeaders(req *http.Request, realIP string) {
	if realIP == "" {
		return
	}
	
	// Set X-Real-IP
	if r.overwriteXRealIP || req.Header.Get(XRealIP) == "" {
		req.Header.Set(XRealIP, realIP)
	}
	
	// Update X-Forwarded-For if it doesn't already contain the real IP
	xff := req.Header.Get(XForwardedFor)
	if xff == "" {
		req.Header.Set(XForwardedFor, realIP)
	} else if !strings.Contains(xff, realIP) {
		req.Header.Set(XForwardedFor, xff+", "+realIP)
	}
}

// setForwardedProto sets X-Forwarded-Proto header
func (r *RealIPOverWriter) setForwardedProto(req *http.Request) {
	if req.Header.Get(XForwardedProto) != "" {
		return
	}
	
	proto := "http"
	if req.TLS != nil {
		proto = "https"
	}
	
	req.Header.Set(XForwardedProto, proto)
}
