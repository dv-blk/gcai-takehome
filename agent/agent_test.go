package agent_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"contractanalyzer/agent"
)

// projectRoot returns the project root directory relative to the test package (agent/).
func projectRoot() string {
	abs, _ := filepath.Abs("..")
	return abs
}

// TestAgent_ReverseWords is an integration test that:
// 1. Starts an isolated Node.js bridge (which starts OpenCode internally)
// 2. Creates an agent pointed at the bridge
// 3. Prompts the agent to read a file and reverse the words
// 4. Verifies the response contains the words in reverse order
func TestAgent_ReverseWords(t *testing.T) {
	// Skip if prerequisites are missing
	if !agent.IsOpenCodeInstalled() {
		t.Skip("Skipping: opencode not installed")
	}
	if !agent.IsNodeInstalled() {
		t.Skip("Skipping: node not installed")
	}
	if !agent.HasCopilotAuth() {
		t.Skip("Skipping: no github-copilot auth found in " + agent.GetDefaultAuthPath())
	}

	// Create context with timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Start isolated bridge + OpenCode server
	t.Log("Starting isolated bridge + OpenCode server...")
	server, err := agent.StartBridge(ctx, t.Logf)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer server.Stop()
	t.Logf("Bridge started at %s", server.BaseURL())

	// Use existing test data file (testdata/example.txt contains "hello world today")
	testDataDir := filepath.Join(projectRoot(), "testdata")

	// Create agent — session working directory is testdata/ so the model
	// can read example.txt via OpenCode's read tool.
	cfg := agent.Config{
		ProviderID: "github-copilot",
		ModelID:    "claude-opus-4.5",
		Directory:  testDataDir,
		BridgeURL:  server.BaseURL(),
	}

	t.Log("Creating agent...")
	ag, err := agent.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}
	defer ag.Close(ctx)

	// Prompt the agent to reverse words — the model will read the file via OpenCode's read tool
	prompt := `Read the file and respond ONLY with the words in reverse order.
No explanation, no extra text, no punctuation - just the words from the file in reverse order, separated by single spaces.
For example, if the file contains "one two three", respond with exactly: three two one`

	t.Log("Sending prompt to agent...")
	response, err := ag.Run(ctx, "example.txt", prompt)
	if err != nil {
		t.Fatalf("Agent.Run failed: %v", err)
	}
	t.Logf("Agent response: %q", response)

	// Normalize whitespace for comparison
	normalize := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
	}

	expected := "today world hello"
	actual := normalize(response)

	if actual != expected {
		t.Errorf("Word reversal mismatch:\n  expected: %q\n  actual:   %q\n  raw response: %q",
			expected, actual, response)
	} else {
		t.Logf("SUCCESS: Agent correctly reversed words: %q", actual)
	}
}
