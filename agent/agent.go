package agent

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// Agent represents an LLM agent session
type Agent struct {
	SessionID string
}

// New creates a new agent with a unique session ID
func New() *Agent {
	return &Agent{
		SessionID: uuid.New().String(),
	}
}

// Run executes the agent with the given contract file path and prompt
// STUB: Returns placeholder text for now
// TODO: Integrate actual LLM here
func (a *Agent) Run(contractFilePath, promptText string) (string, error) {
	// Verify contract file exists (agent would read it in real implementation)
	if _, err := os.Stat(contractFilePath); err != nil {
		return "", fmt.Errorf("contract file not accessible: %w", err)
	}

	// Simulate some processing time (0.5-2 seconds)
	time.Sleep(time.Duration(500+time.Now().UnixNano()%1500) * time.Millisecond)

	// STUB: Return placeholder response
	return fmt.Sprintf(`**Clause:**
"[Placeholder clause from contract]"

**Issue:**
This is a stub response from agent session %s. LLM integration is pending.

**Risk:**
The agent would analyze the contract at: %s

**Fix:**
Integrate actual LLM to provide real analysis based on the prompt.

**Confidence:**
N/A (Stub)
`, a.SessionID[:8], contractFilePath), nil
}
