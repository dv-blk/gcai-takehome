package analyzer

import (
	"context"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"contractanalyzer/agent"
	"contractanalyzer/database"
)

// MaxWorkers is the maximum number of concurrent analysis workers.
// Each worker processes one submission at a time, running all prompts
// sequentially in a single session.
const MaxWorkers = 4

// SubmissionBatch represents all analysis work for a single submission.
// All prompts are run sequentially in the same LLM session.
type SubmissionBatch struct {
	SubmissionID     int64
	ContractFilePath string
	Prompts          []PromptItem // Ordered list of prompts to run
}

// PromptItem holds a single prompt's name, text, and extracted category title
type PromptItem struct {
	Name  string
	Text  string
	Title string // Human-readable category title extracted from prompt text
}

// Queue manages the analysis worker pool
type Queue struct {
	db        *database.DB
	bridgeURL string // URL of the Node.js OpenCode bridge
	batches   chan SubmissionBatch

	mu        sync.RWMutex
	completed map[int64]int // submissionID -> completed prompt count
	total     map[int64]int // submissionID -> total prompt count (NOT including init)
}

var globalQueue *Queue

// InitQueue creates and starts the global queue with workers.
// bridgeURL is the URL of the Node.js OpenCode bridge (e.g., "http://127.0.0.1:9123").
func InitQueue(db *database.DB, bridgeURL string) {
	globalQueue = &Queue{
		db:        db,
		bridgeURL: bridgeURL,
		batches:   make(chan SubmissionBatch, 100),
		completed: make(map[int64]int),
		total:     make(map[int64]int),
	}

	// Start workers
	for i := 0; i < MaxWorkers; i++ {
		go globalQueue.worker(i)
	}

	log.Printf("Analysis queue initialized with %d workers (session persistence enabled)", MaxWorkers)
}

// worker processes submission batches from the queue.
// Each batch runs all prompts sequentially in a single session.
func (q *Queue) worker(id int) {
	for batch := range q.batches {
		log.Printf("Worker %d processing batch [submission=%d, prompts=%d]",
			id, batch.SubmissionID, len(batch.Prompts))

		ctx := context.Background()
		cfg := agent.DefaultConfig()
		cfg.BridgeURL = q.bridgeURL

		// Set the session's working directory.
		// In Docker, OPENCODE_PROJECT_DIR points to the OpenCode server's project root (/workspace).
		// Locally, we default to the current working directory.
		if projectDir := os.Getenv("OPENCODE_PROJECT_DIR"); projectDir != "" {
			cfg.Directory = projectDir
		} else {
			// Default: use current working directory (project root in local dev)
			if cwd, err := os.Getwd(); err == nil {
				cfg.Directory = cwd
			}
		}

		ag, err := agent.New(ctx, cfg)
		if err != nil {
			log.Printf("Worker %d failed to create agent: %v", id, err)
			q.failSubmission(batch.SubmissionID, "Failed to initialize analysis agent")
			continue
		}

		// Process the batch with session persistence
		q.processBatch(ctx, ag, batch)
	}
}

// processBatch runs all prompts for a submission in a single session,
// streaming results to the database after each prompt completes.
func (q *Queue) processBatch(ctx context.Context, ag *agent.Agent, batch SubmissionBatch) {
	var sections []string // Accumulated result sections with headers
	var sessionID string

	// Cleanup session on exit (even if we fail partway through)
	defer func() {
		if sessionID != "" {
			if err := ag.DeleteSession(ctx, sessionID); err != nil {
				log.Printf("Failed to delete session %s: %v", sessionID, err)
			} else {
				log.Printf("Session %s deleted", sessionID)
			}
		}
	}()

	// Step 1: Initialize session (reads the contract file)
	// This does NOT count toward progress — it's just setup
	// Use the relative path (e.g., "data/contracts/contract_1_xxx.txt") so it resolves
	// correctly from the project root in both local dev and Docker.
	log.Printf("Initializing session for submission %d", batch.SubmissionID)
	var err error
	sessionID, err = ag.InitSession(ctx, batch.ContractFilePath)
	if err != nil {
		log.Printf("Failed to initialize session for submission %d: %v", batch.SubmissionID, err)
		q.failSubmission(batch.SubmissionID, "Failed to read contract file")
		return
	}
	log.Printf("Session initialized: %s", sessionID)

	// Step 2: Run each prompt sequentially in the same session
	for i, prompt := range batch.Prompts {
		log.Printf("Running prompt %d/%d [%s] for submission %d",
			i+1, len(batch.Prompts), prompt.Name, batch.SubmissionID)

		output, _, err := ag.RunInSession(ctx, sessionID, prompt.Text)
		if err != nil {
			log.Printf("Prompt %s failed for submission %d: %v", prompt.Name, batch.SubmissionID, err)
			// Continue with other prompts even if one fails
		} else {
			log.Printf("Prompt %s completed for submission %d", prompt.Name, batch.SubmissionID)

			// Build section with header and proper spacing
			section := "## " + prompt.Title + "\n\n" + strings.TrimSpace(output)
			sections = append(sections, section)

			// Stream partial result to database
			// Use extra line breaks between sections for visual separation
			combined := strings.Join(sections, "\n\n---\n\n")
			if err := q.db.UpdateSubmissionPartialResult(batch.SubmissionID, combined); err != nil {
				log.Printf("Failed to save partial result for submission %d: %v", batch.SubmissionID, err)
			}
		}

		// Update progress (only counts prompts, not init)
		q.mu.Lock()
		q.completed[batch.SubmissionID] = i + 1
		q.mu.Unlock()
	}

	// Step 3: Finalize - flip status to complete
	if len(sections) == 0 {
		// All prompts failed
		if err := q.db.UpdateSubmissionAnalysis(batch.SubmissionID, "Analysis could not be completed.", "complete"); err != nil {
			log.Printf("Failed to save error for submission %d: %v", batch.SubmissionID, err)
		}
		log.Printf("All prompts failed for submission %d", batch.SubmissionID)
	} else {
		// Just flip the status — results are already in the DB
		if err := q.db.UpdateSubmissionStatus(batch.SubmissionID, "complete"); err != nil {
			log.Printf("Failed to update status for submission %d: %v", batch.SubmissionID, err)
		}
		log.Printf("Analysis complete for submission %d (%d results)", batch.SubmissionID, len(sections))
	}

	// Cleanup progress tracking
	q.mu.Lock()
	delete(q.completed, batch.SubmissionID)
	delete(q.total, batch.SubmissionID)
	q.mu.Unlock()
}

// failSubmission marks a submission as complete with an error message
func (q *Queue) failSubmission(submissionID int64, message string) {
	if err := q.db.UpdateSubmissionAnalysis(submissionID, message, "complete"); err != nil {
		log.Printf("Failed to save error for submission %d: %v", submissionID, err)
	}

	q.mu.Lock()
	delete(q.completed, submissionID)
	delete(q.total, submissionID)
	q.mu.Unlock()
}

// extractCategoryTitle extracts the category name from a prompt's text.
// Prompts start with: "Now analyze the contract for aggressive **Category Name** clauses."
// Returns the text between ** and **, or falls back to title-casing the prompt name.
func extractCategoryTitle(promptName, promptText string) string {
	// Try to extract from **...** pattern
	re := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	matches := re.FindStringSubmatch(promptText)
	if len(matches) >= 2 {
		return matches[1]
	}

	// Fallback: title-case the filename
	// "pricing_payment_terms" -> "Pricing Payment Terms"
	words := strings.Split(promptName, "_")
	for i, word := range words {
		if len(word) > 0 {
			// Handle common acronyms
			upper := strings.ToUpper(word)
			if upper == "SLA" || upper == "DR" || upper == "IP" || upper == "DPA" {
				words[i] = upper
			} else {
				words[i] = strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
			}
		}
	}
	return strings.Join(words, " ")
}

// SubmitAnalysis enqueues a submission batch for analysis.
// Returns immediately (async processing).
func SubmitAnalysis(submissionID int64, contractFilePath string) error {
	prompts, err := DiscoverPrompts()
	if err != nil {
		return err
	}

	if len(prompts) == 0 {
		log.Printf("No analysis prompts configured for submission %d", submissionID)
		return globalQueue.db.UpdateSubmissionAnalysis(submissionID,
			"No analysis prompts configured. Add prompt files to the prompts/ directory.", "complete")
	}

	// Convert map to ordered slice (sort by name for consistent ordering)
	promptList := make([]PromptItem, 0, len(prompts))
	for name, text := range prompts {
		promptList = append(promptList, PromptItem{
			Name:  name,
			Text:  text,
			Title: extractCategoryTitle(name, text),
		})
	}
	sort.Slice(promptList, func(i, j int) bool {
		return promptList[i].Name < promptList[j].Name
	})

	// Track progress: total = number of prompts (NOT including init step)
	globalQueue.mu.Lock()
	globalQueue.total[submissionID] = len(promptList)
	globalQueue.completed[submissionID] = 0
	globalQueue.mu.Unlock()

	log.Printf("Submitting batch for submission %d (%d prompts)", submissionID, len(promptList))

	// Enqueue the batch
	globalQueue.batches <- SubmissionBatch{
		SubmissionID:     submissionID,
		ContractFilePath: contractFilePath,
		Prompts:          promptList,
	}

	return nil
}

// GetProgress returns the analysis progress for a submission.
// Returns (completed, total) counts — does NOT include the init step.
func GetProgress(submissionID int64) (completed, total int) {
	globalQueue.mu.RLock()
	defer globalQueue.mu.RUnlock()

	t, exists := globalQueue.total[submissionID]
	if !exists {
		return 0, 0
	}

	return globalQueue.completed[submissionID], t
}
