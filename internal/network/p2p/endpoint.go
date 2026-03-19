package p2p

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

func normalizePeerBaseURL(ctx context.Context, raw string, allowPrivate bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("peer listen_addr is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("peer listen_addr must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("peer listen_addr host is required")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("peer listen_addr must not contain embedded credentials")
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("peer listen_addr host is required")
	}
	if !allowPrivate {
		if err := validatePublicPeerHost(ctx, host); err != nil {
			return "", err
		}
	}

	canonical := url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
	}
	return strings.TrimRight(canonical.String(), "/"), nil
}

func validatePublicPeerHost(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicPeerIP(ip) {
			return fmt.Errorf("peer listen_addr must not resolve to a private or local address")
		}
		return nil
	}

	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve peer listen_addr host: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("peer listen_addr host did not resolve")
	}
	for _, address := range addresses {
		if !isPublicPeerIP(address.IP) {
			return fmt.Errorf("peer listen_addr must not resolve to a private or local address")
		}
	}
	return nil
}

func isPublicPeerIP(ip net.IP) bool {
	return ip != nil &&
		!ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsLinkLocalUnicast()
}
