// Package client provides a thin HTTP client for the Mantismo internal API server.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/abidkhan1974/mantismo/internal/api"
	"github.com/abidkhan1974/mantismo/internal/logger"
)

// Client is a thin HTTP client for the internal API server.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a client pointing at the given port on localhost.
func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Health checks if the API server is reachable.
func (c *Client) Health() error {
	resp, err := c.http.Get(c.baseURL + "/api/health")
	if err != nil {
		return fmt.Errorf("api not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api health check: status %d", resp.StatusCode)
	}
	return nil
}

// LogFilter holds query parameters for the logs endpoint.
type LogFilter struct {
	Since    string
	Until    string
	Tool     string
	Method   string
	Session  string
	Decision string
	Limit    int
}

// Logs queries the audit log from the API server.
func (c *Client) Logs(filter LogFilter) ([]logger.LogEntry, error) {
	u := c.baseURL + "/api/logs?" + buildQuery(filter)
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var entries []logger.LogEntry
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

// ToolInfo is the API representation of a tool with fingerprint status.
type ToolInfo struct {
	Name         string    `json:"name"`
	Hash         string    `json:"hash"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	ServerCmd    string    `json:"server_cmd"`
	Acknowledged bool      `json:"acknowledged"`
	Changed      bool      `json:"changed"`
}

// Tools returns the tool list from the API.
func (c *Client) Tools() ([]ToolInfo, error) {
	resp, err := c.http.Get(c.baseURL + "/api/tools")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tools []ToolInfo
	return tools, json.NewDecoder(resp.Body).Decode(&tools)
}

// StatsResponse is the shape of the /api/stats response.
type StatsResponse struct {
	ToolCallsToday int              `json:"tool_calls_today"`
	BlockedToday   int              `json:"blocked_today"`
	SessionsToday  int              `json:"sessions_today"`
	ActiveSession  *api.SessionInfo `json:"active_session"`
}

// Stats returns dashboard statistics.
func (c *Client) Stats() (*StatsResponse, error) {
	resp, err := c.http.Get(c.baseURL + "/api/stats")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var stats StatsResponse
	return &stats, json.NewDecoder(resp.Body).Decode(&stats)
}

// StreamLogs connects to the WebSocket log stream and sends entries to ch.
// It blocks until ctx is cancelled or the connection is closed.
func (c *Client) StreamLogs(ctx context.Context, ch chan<- logger.LogEntry) error {
	wsURL := "ws://127.0.0.1:" + portFromBase(c.baseURL) + "/api/ws/logs"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil
		}
		var entry logger.LogEntry
		if err := json.Unmarshal(msg, &entry); err == nil {
			select {
			case ch <- entry:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// buildQuery converts a LogFilter into a URL query string.
func buildQuery(f LogFilter) string {
	var parts []string
	if f.Since != "" {
		parts = append(parts, "since="+f.Since)
	}
	if f.Until != "" {
		parts = append(parts, "until="+f.Until)
	}
	if f.Tool != "" {
		parts = append(parts, "tool="+f.Tool)
	}
	if f.Method != "" {
		parts = append(parts, "method="+f.Method)
	}
	if f.Session != "" {
		parts = append(parts, "session="+f.Session)
	}
	if f.Decision != "" {
		parts = append(parts, "decision="+f.Decision)
	}
	if f.Limit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%d", f.Limit))
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "&"
		}
		result += p
	}
	return result
}

// portFromBase extracts the port string from a base URL like "http://127.0.0.1:7777".
func portFromBase(base string) string {
	idx := len(base) - 1
	for idx >= 0 && base[idx] != ':' {
		idx--
	}
	if idx < 0 {
		return "7777"
	}
	return base[idx+1:]
}
