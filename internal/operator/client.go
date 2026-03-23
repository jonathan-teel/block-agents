package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aichain/internal/protocol"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type apiError struct {
	StatusCode int
	Message    string
	ErrorCode  string
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	if e.ErrorCode == "" {
		return fmt.Sprintf("rpc status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("rpc status %d: %s (%s)", e.StatusCode, e.Message, e.ErrorCode)
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: normalizeBaseURL(baseURL),
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) ChainInfo(ctx context.Context) (protocol.ChainInfo, error) {
	var value protocol.ChainInfo
	return value, c.getJSON(ctx, "/v1/chain/info", &value)
}

func (c *Client) HeadBlock(ctx context.Context) (protocol.Block, error) {
	var value protocol.Block
	return value, c.getJSON(ctx, "/v1/blocks/head", &value)
}

func (c *Client) BlockByHeight(ctx context.Context, height int64) (protocol.Block, error) {
	var value protocol.Block
	path := "/v1/blocks/" + strconv.FormatInt(height, 10)
	return value, c.getJSON(ctx, path, &value)
}

func (c *Client) Transaction(ctx context.Context, hash string) (protocol.TransactionStatus, error) {
	var value protocol.TransactionStatus
	return value, c.getJSON(ctx, "/v1/txs/"+strings.TrimSpace(hash), &value)
}

func (c *Client) Task(ctx context.Context, id string) (protocol.TaskDetails, error) {
	var value protocol.TaskDetails
	return value, c.getJSON(ctx, "/v1/tasks/"+strings.TrimSpace(id), &value)
}

func (c *Client) Agent(ctx context.Context, address string) (protocol.Agent, error) {
	var value protocol.Agent
	return value, c.getJSON(ctx, "/v1/agents/"+strings.TrimSpace(address), &value)
}

func (c *Client) Peers(ctx context.Context) ([]protocol.PeerStatus, error) {
	var value []protocol.PeerStatus
	return value, c.getJSON(ctx, "/v1/p2p/peers", &value)
}

func (c *Client) Validators(ctx context.Context) ([]protocol.Validator, error) {
	var value []protocol.Validator
	return value, c.getJSON(ctx, "/v1/consensus/validators", &value)
}

func (c *Client) OpenTasks(ctx context.Context) ([]protocol.Task, error) {
	var value []protocol.Task
	return value, c.getJSON(ctx, "/v1/tasks/open", &value)
}

func (c *Client) SyncStatus(ctx context.Context) (protocol.SyncStatus, error) {
	var value protocol.SyncStatus
	return value, c.getJSON(ctx, "/v1/sync/status", &value)
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var payload struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return &apiError{
			StatusCode: resp.StatusCode,
			Message:    payload.Error,
			ErrorCode:  payload.ErrorCode,
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return &apiError{
		StatusCode: resp.StatusCode,
		Message:    message,
	}
}

func normalizeBaseURL(value string) string {
	baseURL := strings.TrimSpace(value)
	if baseURL == "" {
		baseURL = defaultRPCURL()
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return strings.TrimRight(baseURL, "/")
}
