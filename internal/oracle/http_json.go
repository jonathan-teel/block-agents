package oracle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"aichain/internal/protocol"
)

const HTTPJSONSource = "http_json"

type HTTPJSONAdapter struct {
	client              *http.Client
	allowPrivateTargets bool
}

func NewHTTPJSONAdapter(timeoutSeconds int, allowPrivateTargets bool) *HTTPJSONAdapter {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	adapter := &HTTPJSONAdapter{
		allowPrivateTargets: allowPrivateTargets,
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return adapter.validateURL(req.Context(), req.URL.String())
	}
	adapter.client = client
	return adapter
}

func (a *HTTPJSONAdapter) Name() string {
	return HTTPJSONSource
}

func (a *HTTPJSONAdapter) Resolve(ctx context.Context, task protocol.Task) (Result, error) {
	endpoint := strings.TrimSpace(task.Input.OracleEndpoint)
	path := strings.TrimSpace(task.Input.OraclePath)
	if endpoint == "" || path == "" {
		return Result{}, fmt.Errorf("oracle endpoint and oracle path are required")
	}
	if err := a.validateURL(ctx, endpoint); err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build oracle request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "blockagents-oracle/0.1")

	resp, err := a.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch oracle response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("oracle endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read oracle response: %w", err)
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Result{}, fmt.Errorf("decode oracle response: %w", err)
	}

	value, err := extractNumericPath(payload, path)
	if err != nil {
		return Result{}, err
	}
	if value < 0 || value > 1 {
		return Result{}, fmt.Errorf("oracle value must be within [0,1]")
	}

	sum := sha256.Sum256(body)
	return Result{
		Value:      value,
		ObservedAt: time.Now().UTC().Unix(),
		RawHash:    hex.EncodeToString(sum[:]),
	}, nil
}

func extractNumericPath(root any, path string) (float64, error) {
	current := root
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return 0, fmt.Errorf("oracle path contains an empty segment")
		}
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[segment]
			if !ok {
				return 0, fmt.Errorf("oracle path segment %q not found", segment)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return 0, fmt.Errorf("oracle array segment %q is invalid", segment)
			}
			current = node[index]
		default:
			return 0, fmt.Errorf("oracle path segment %q is not addressable", segment)
		}
	}

	switch value := current.(type) {
	case float64:
		return value, nil
	case json.Number:
		return value.Float64()
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, fmt.Errorf("oracle path value is not numeric")
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("oracle path did not resolve to a numeric value")
	}
}

func (a *HTTPJSONAdapter) validateURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("oracle endpoint is invalid: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("oracle endpoint must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("oracle endpoint host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("oracle endpoint must not contain embedded credentials")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("oracle endpoint host is required")
	}
	if a.allowPrivateTargets {
		return nil
	}
	return validatePublicHost(ctx, host)
}

func validatePublicHost(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("oracle endpoint host must not resolve to a private or local address")
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve oracle endpoint host: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("oracle endpoint host did not resolve")
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return fmt.Errorf("oracle endpoint host must not resolve to a private or local address")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil &&
		!ip.IsLoopback() &&
		!ip.IsPrivate() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsLinkLocalUnicast()
}
