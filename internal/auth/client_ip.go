package auth

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

type trustedProxySet []netip.Prefix

func (s *Service) ConfigureTrustedProxies(value string) error {
	prefixes := make(trustedProxySet, 0)
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := parseTrustedProxyPrefix(raw)
		if err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		prefixes = append(prefixes, prefix)
	}
	s.trustedProxies = prefixes
	return nil
}

func parseTrustedProxyPrefix(value string) (netip.Prefix, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	address = address.Unmap()
	return netip.PrefixFrom(address, address.BitLen()), nil
}

func (s *Service) clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	peer, ok := parseClientAddress(r.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if !s.trustedProxies.contains(peer) {
		return peer.String()
	}

	chain := forwardedForChain(r.Header.Values("Forwarded"))
	if len(chain) == 0 {
		chain = xForwardedForChain(r.Header.Values("X-Forwarded-For"))
	}
	if len(chain) == 0 {
		if realIP, ok := parseClientAddress(r.Header.Get("X-Real-IP")); ok {
			chain = append(chain, realIP)
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !s.trustedProxies.contains(chain[index]) {
			return chain[index].String()
		}
	}
	if len(chain) > 0 {
		return chain[0].String()
	}
	return peer.String()
}

func (set trustedProxySet) contains(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range set {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func forwardedForChain(values []string) []netip.Addr {
	chain := make([]netip.Addr, 0)
	for _, value := range values {
		for _, element := range strings.Split(value, ",") {
			for _, parameter := range strings.Split(element, ";") {
				key, raw, ok := strings.Cut(parameter, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
					continue
				}
				if address, ok := parseClientAddress(raw); ok {
					chain = append(chain, address)
				}
				break
			}
		}
	}
	return chain
}

func xForwardedForChain(values []string) []netip.Addr {
	chain := make([]netip.Addr, 0)
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if address, ok := parseClientAddress(raw); ok {
				chain = append(chain, address)
			}
		}
	}
	return chain
}

func parseClientAddress(value string) (netip.Addr, bool) {
	value = strings.Trim(strings.TrimSpace(value), "\"")
	if value == "" || strings.EqualFold(value, "unknown") || strings.HasPrefix(value, "_") {
		return netip.Addr{}, false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap(), true
	}
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr().Unmap(), true
	}
	return netip.Addr{}, false
}
