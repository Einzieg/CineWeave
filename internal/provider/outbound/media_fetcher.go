package outbound

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/observability"
)

const (
	defaultMaxRedirects          = 3
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultTLSHandshakeTimeout   = 15 * time.Second
)

var (
	ErrBlockedAddress   = errors.New("provider media address is blocked")
	ErrInvalidURL       = errors.New("provider media URL is invalid")
	ErrMIMETypeRejected = errors.New("provider media MIME type is not allowed")
	ErrResponseTooLarge = errors.New("provider media response exceeds the byte limit")
)

// Resolver is deliberately narrower than net.Resolver so tests can prove that
// the address set validated by the policy is the same set used by the dialer.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type NetworkPolicy struct {
	AllowedPrivateHosts []string
	AllowedPrivateCIDRs []netip.Prefix
}

type RequestPolicy = NetworkPolicy

type FetchOptions struct {
	Kind                  string
	MaxBytes              int64
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	UpstreamMIMEType      string
	AllowedMIMEPrefixes   []string
	Policy                NetworkPolicy
}

type Result struct {
	Path        string
	FinalURL    string
	MIMEType    string
	ByteSize    int64
	ContentHash string
	Redirects   int
}

func (r Result) Close() error {
	if strings.TrimSpace(r.Path) == "" {
		return nil
	}
	err := os.Remove(r.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type Config struct {
	Resolver     Resolver
	Dialer       Dialer
	TempDir      string
	MaxRedirects int
}

type MediaFetcher struct {
	resolver     Resolver
	dialer       Dialer
	tempDir      string
	maxRedirects int
}

func NewMediaFetcher(config Config) *MediaFetcher {
	resolver := config.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	}
	maxRedirects := config.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = defaultMaxRedirects
	}
	return &MediaFetcher{
		resolver:     resolver,
		dialer:       dialer,
		tempDir:      strings.TrimSpace(config.TempDir),
		maxRedirects: maxRedirects,
	}
}

func (f *MediaFetcher) FetchToTempFile(ctx context.Context, rawURL string, options FetchOptions) (result Result, err error) {
	redirects := 0
	defer func() {
		if err != nil {
			if reason := mediaPolicyFailureReason(err); reason != "" {
				observability.RecordProviderMediaPolicyRejection(options.Kind, reason)
			}
			return
		}
		observability.RecordProviderMediaDownload(options.Kind, result.ByteSize, result.Redirects)
	}()
	if f == nil {
		return Result{}, fmt.Errorf("media fetcher is not configured")
	}
	if options.MaxBytes <= 0 {
		return Result{}, fmt.Errorf("media byte limit must be positive")
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}
	if options.ResponseHeaderTimeout <= 0 {
		options.ResponseHeaderTimeout = defaultResponseHeaderTimeout
	}

	requestContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()

	transport := &policyTransport{
		resolver:              f.resolver,
		dialer:                f.dialer,
		policy:                options.Policy,
		responseHeaderTimeout: options.ResponseHeaderTimeout,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirects = len(via)
			if len(via) >= f.maxRedirects {
				return fmt.Errorf("provider media redirect limit exceeded")
			}
			if len(via) > 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("provider media redirect cannot downgrade HTTPS to HTTP")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf("provider %s download failed: status=%d", normalizedKind(options.Kind), resp.StatusCode)
	}
	if resp.ContentLength > options.MaxBytes {
		return Result{}, fmt.Errorf("%w: provider %s exceeds %d bytes", ErrResponseTooLarge, normalizedKind(options.Kind), options.MaxBytes)
	}
	result, err = f.spoolReaderToTempFile(resp.Body, resp.Header.Get("Content-Type"), resp.Request.URL, options)
	result.Redirects = redirects
	return result, err
}

// SpoolToTempFile applies the same size, MIME and hashing rules to a trusted
// upstream response body, such as a binary TTS response returned directly by
// the configured provider endpoint.
func (f *MediaFetcher) SpoolToTempFile(ctx context.Context, reader io.Reader, options FetchOptions) (result Result, err error) {
	defer func() {
		if err != nil {
			if reason := mediaPolicyFailureReason(err); reason != "" {
				observability.RecordProviderMediaPolicyRejection(options.Kind, reason)
			}
			return
		}
		observability.RecordProviderMediaDownload(options.Kind, result.ByteSize, 0)
	}()
	if f == nil {
		return Result{}, fmt.Errorf("media fetcher is not configured")
	}
	if reader == nil || options.MaxBytes <= 0 {
		return Result{}, fmt.Errorf("media reader and positive byte limit are required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}
	spoolContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	return f.spoolReaderToTempFile(&contextReader{ctx: spoolContext, reader: reader}, "", nil, options)
}

func mediaPolicyFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrBlockedAddress):
		return "blocked_address"
	case errors.Is(err, ErrInvalidURL):
		return "invalid_url"
	case errors.Is(err, ErrMIMETypeRejected):
		return "mime_type"
	case errors.Is(err, ErrResponseTooLarge):
		return "byte_limit"
	case strings.Contains(strings.ToLower(err.Error()), "redirect"):
		return "redirect"
	default:
		return ""
	}
}

func (f *MediaFetcher) spoolReaderToTempFile(reader io.Reader, responseMIMEType string, finalURL *url.URL, options FetchOptions) (Result, error) {
	tempFile, err := os.CreateTemp(f.tempDir, "cineweave-provider-media-*")
	if err != nil {
		return Result{}, err
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tempFile, hasher), io.LimitReader(reader, options.MaxBytes+1))
	if err != nil {
		return Result{}, err
	}
	if written > options.MaxBytes {
		return Result{}, fmt.Errorf("%w: provider %s exceeds %d bytes", ErrResponseTooLarge, normalizedKind(options.Kind), options.MaxBytes)
	}
	if err := tempFile.Sync(); err != nil {
		return Result{}, err
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return Result{}, err
	}
	prefix := make([]byte, 512)
	prefixLength, readErr := io.ReadFull(tempFile, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return Result{}, readErr
	}
	prefix = prefix[:prefixLength]

	mimeType := selectMIMEType(responseMIMEType, options.UpstreamMIMEType, finalURL, prefix)
	detectedMIMEType := normalizeMIMEType(http.DetectContentType(prefix))
	if detectedMIMEType != "" && detectedMIMEType != "application/octet-stream" {
		if err := validateMIMEType(detectedMIMEType, options.AllowedMIMEPrefixes); err != nil {
			return Result{}, fmt.Errorf("provider media content does not match its declared type: %w", err)
		}
	}
	if err := validateMIMEType(mimeType, options.AllowedMIMEPrefixes); err != nil {
		return Result{}, err
	}
	if err := tempFile.Close(); err != nil {
		return Result{}, err
	}
	removeTemp = false
	result := Result{
		Path:        tempPath,
		MIMEType:    mimeType,
		ByteSize:    written,
		ContentHash: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
	}
	if finalURL != nil {
		result.FinalURL = finalURL.String()
	}
	return result, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}

type policyTransport struct {
	resolver              Resolver
	dialer                Dialer
	policy                NetworkPolicy
	responseHeaderTimeout time.Duration
}

func (t *policyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host, addresses, err := t.resolveAndValidate(req.Context(), req.URL, t.policy)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: t.responseHeaderTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		requestedHost, port, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, splitErr
		}
		if !sameHost(requestedHost, host) {
			return nil, fmt.Errorf("provider media dial host changed after validation")
		}
		var dialErrors []error
		for _, addressValue := range addresses {
			connection, dialErr := t.dialer.DialContext(ctx, network, net.JoinHostPort(addressValue.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}
		return nil, errors.Join(dialErrors...)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	resp.Body = &transportResponseBody{ReadCloser: resp.Body, transport: transport}
	return resp, nil
}

func (t *policyTransport) resolveAndValidate(ctx context.Context, target *url.URL, policy NetworkPolicy) (string, []netip.Addr, error) {
	host, err := validateURL(target)
	if err != nil {
		return "", nil, err
	}
	if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
		parsed = parsed.Unmap()
		if err := validateAddress(host, parsed, policy); err != nil {
			return "", nil, err
		}
		return host, []netip.Addr{parsed}, nil
	}
	addresses, err := t.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		if err == nil {
			err = errors.New("no addresses returned")
		}
		return "", nil, fmt.Errorf("provider media host %q could not be resolved: %w", host, err)
	}
	validated := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() {
			return "", nil, fmt.Errorf("%w: invalid resolved address", ErrBlockedAddress)
		}
		if err := validateAddress(host, address, policy); err != nil {
			return "", nil, err
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		validated = append(validated, address)
	}
	return host, validated, nil
}

type transportResponseBody struct {
	io.ReadCloser
	transport *http.Transport
}

func (b *transportResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.transport.CloseIdleConnections()
	return err
}

func validateURL(target *url.URL) (string, error) {
	if target == nil || target.Host == "" {
		return "", ErrInvalidURL
	}
	if target.User != nil {
		return "", fmt.Errorf("%w: userinfo is not allowed", ErrInvalidURL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return "", fmt.Errorf("%w: only HTTP and HTTPS are allowed", ErrInvalidURL)
	}
	host := strings.ToLower(target.Hostname())
	if host == "" || strings.HasSuffix(host, ".") || strings.Contains(host, "%") {
		return "", fmt.Errorf("%w: hostname is not canonical", ErrInvalidURL)
	}
	if port := target.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("%w: port is invalid", ErrInvalidURL)
		}
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return host, nil
	}
	if err := validateDNSName(host); err != nil {
		return "", err
	}
	return host, nil
}

func validateDNSName(host string) error {
	if len(host) > 253 {
		return fmt.Errorf("%w: hostname is too long", ErrInvalidURL)
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("%w: hostname is not canonical", ErrInvalidURL)
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return fmt.Errorf("%w: hostname is not canonical", ErrInvalidURL)
		}
	}
	return nil
}

func validateAddress(host string, address netip.Addr, policy NetworkPolicy) error {
	if address.IsGlobalUnicast() && !isDeniedSpecialAddress(address) {
		return nil
	}
	if privateAddressAllowed(host, address, policy) {
		return nil
	}
	return fmt.Errorf("%w: host %q resolves to %s", ErrBlockedAddress, host, address)
}

func isDeniedSpecialAddress(address netip.Addr) bool {
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified()
}

func privateAddressAllowed(host string, address netip.Addr, policy NetworkPolicy) bool {
	hostAllowed := false
	for _, allowedHost := range policy.AllowedPrivateHosts {
		if sameHost(host, strings.TrimSpace(allowedHost)) {
			hostAllowed = true
			break
		}
	}
	addressAllowed := false
	for _, prefix := range policy.AllowedPrivateCIDRs {
		if prefix.IsValid() && prefix.Contains(address) {
			addressAllowed = true
			break
		}
	}
	return hostAllowed && addressAllowed
}

func sameHost(left, right string) bool {
	return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(left), "."), strings.TrimSuffix(strings.TrimSpace(right), "."))
}

func selectMIMEType(responseValue, upstreamValue string, finalURL *url.URL, prefix []byte) string {
	candidates := []string{responseValue, upstreamValue}
	for _, candidate := range candidates {
		if normalized := normalizeMIMEType(candidate); normalized != "" && normalized != "application/octet-stream" {
			return normalized
		}
	}
	if detected := normalizeMIMEType(http.DetectContentType(prefix)); detected != "" && detected != "application/octet-stream" {
		return detected
	}
	if finalURL != nil {
		if inferred := normalizeMIMEType(mime.TypeByExtension(filepath.Ext(finalURL.Path))); inferred != "" {
			return inferred
		}
	}
	return "application/octet-stream"
}

func validateMIMEType(value string, allowedPrefixes []string) error {
	if len(allowedPrefixes) == 0 {
		return nil
	}
	for _, prefix := range allowedPrefixes {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix != "" && strings.HasPrefix(value, prefix) {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrMIMETypeRejected, value)
}

func normalizeMIMEType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err == nil {
		return strings.ToLower(strings.TrimSpace(parsed))
	}
	if separator := strings.IndexByte(value, ';'); separator >= 0 {
		value = value[:separator]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedKind(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "media"
	}
	return value
}

var deniedPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
