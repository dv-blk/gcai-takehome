package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds agent configuration
type Config struct {
	ProviderID string // e.g., "github-copilot"
	ModelID    string // e.g., "claude-sonnet-4"
	Directory  string // Working directory for the session
	BridgeURL  string // URL of the Node.js bridge (e.g., "http://127.0.0.1:9123")
}

// DefaultConfig returns configuration for github-copilot/claude-sonnet-4
func DefaultConfig() Config {
	return Config{
		ProviderID: "github-copilot",
		ModelID:    "claude-sonnet-4",
	}
}

// Agent wraps the Node.js OpenCode bridge for LLM interactions
type Agent struct {
	config Config
	client *http.Client
}

// New creates a new agent with the specified configuration
func New(ctx context.Context, cfg Config) (*Agent, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("BridgeURL is required")
	}
	return &Agent{
		config: cfg,
		client: &http.Client{
			Timeout: 3 * time.Minute, // Must exceed bridge-side prompt timeout (120s) to avoid premature cutoff
		},
	}, nil
}

// promptRequest is the JSON body sent to the bridge's /prompt endpoint
type promptRequest struct {
	Text          string `json:"text"`
	ProviderID    string `json:"providerID,omitempty"`
	ModelID       string `json:"modelID,omitempty"`
	Directory     string `json:"directory,omitempty"`
	SessionID     string `json:"sessionID,omitempty"`     // Reuse existing session if provided
	DeleteSession *bool  `json:"deleteSession,omitempty"` // If false, preserve session after prompt
}

// promptResponse is the JSON body returned from the bridge's /prompt endpoint
type promptResponse struct {
	Text      string `json:"text"`
	SessionID string `json:"sessionID"`
	Error     string `json:"error,omitempty"`
}

// Run sends a prompt referencing a file path and returns the agent's response.
// The model reads the file via OpenCode's read tool. filePath can be relative
// (resolved against the session's working directory) or absolute.
// This is a single-shot call that creates and destroys a session.
func (a *Agent) Run(ctx context.Context, filePath, promptText string) (string, error) {
	fullPrompt := fmt.Sprintf("Read the file at %s then:\n\n%s", filePath, promptText)

	reqBody := promptRequest{
		Text:       fullPrompt,
		ProviderID: a.config.ProviderID,
		ModelID:    a.config.ModelID,
		Directory:  a.config.Directory,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.BridgeURL+"/prompt", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result promptResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bridge error (HTTP %d): %s", resp.StatusCode, result.Error)
	}

	if result.Text == "" {
		return "", fmt.Errorf("empty response from bridge")
	}

	return result.Text, nil
}

// InitSession creates a new session that reads and understands a contract file.
// Returns the sessionID for use in subsequent RunInSession calls.
// The session is NOT deleted after this call - caller must call DeleteSession when done.
func (a *Agent) InitSession(ctx context.Context, filePath string) (string, error) {
	initPrompt := fmt.Sprintf(`Read the file at %s. You are a contract clause analyst specializing in identifying aggressive vendor terms. Confirm you have read and understood the contract. Do not begin analysis yet — you will receive specific analysis instructions next.`, filePath)

	deleteSession := false
	reqBody := promptRequest{
		Text:          initPrompt,
		ProviderID:    a.config.ProviderID,
		ModelID:       a.config.ModelID,
		Directory:     a.config.Directory,
		DeleteSession: &deleteSession,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.BridgeURL+"/prompt", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result promptResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bridge error (HTTP %d): %s", resp.StatusCode, result.Error)
	}

	if result.SessionID == "" {
		return "", fmt.Errorf("no sessionID returned from bridge")
	}

	return result.SessionID, nil
}

// RunInSession sends a follow-up prompt to an existing session.
// The session is NOT deleted after this call - caller must call DeleteSession when done.
// Returns the response text and the sessionID (same as input, for convenience).
func (a *Agent) RunInSession(ctx context.Context, sessionID, promptText string) (string, string, error) {
	if sessionID == "" {
		return "", "", fmt.Errorf("sessionID is required for RunInSession")
	}

	deleteSession := false
	reqBody := promptRequest{
		Text:          promptText,
		ProviderID:    a.config.ProviderID,
		ModelID:       a.config.ModelID,
		Directory:     a.config.Directory,
		SessionID:     sessionID,
		DeleteSession: &deleteSession,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.BridgeURL+"/prompt", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response: %w", err)
	}

	var result promptResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("bridge error (HTTP %d): %s", resp.StatusCode, result.Error)
	}

	if result.Text == "" {
		return "", "", fmt.Errorf("empty response from bridge")
	}

	return result.Text, result.SessionID, nil
}

// deleteSessionRequest is the JSON body sent to DELETE /session
type deleteSessionRequest struct {
	SessionID string `json:"sessionID"`
	Directory string `json:"directory,omitempty"`
}

// DeleteSession explicitly deletes a session on the bridge.
// Call this when done with a session created by InitSession/RunInSession.
func (a *Agent) DeleteSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil // Nothing to delete
	}

	reqBody := deleteSessionRequest{
		SessionID: sessionID,
		Directory: a.config.Directory,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", a.config.BridgeURL+"/session", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bridge error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SessionID is no longer tracked client-side (bridge manages sessions).
func (a *Agent) SessionID() string {
	return ""
}

// Close is a no-op -- the bridge handles session cleanup.
func (a *Agent) Close(ctx context.Context) error {
	return nil
}
