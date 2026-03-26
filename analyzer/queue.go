package analyzer

import (
	"log"
	"strings"
	"sync"

	"contractanalyzer/agent"
	"contractanalyzer/database"
)

// MaxWorkers is the maximum number of concurrent analysis workers
const MaxWorkers = 4

// AnalysisJob represents a single analysis task
type AnalysisJob struct {
	SubmissionID     int64
	ContractFilePath string
	PromptName       string
	PromptText       string
}

// JobResult holds the outcome of a job
type JobResult struct {
	SubmissionID int64
	PromptName   string
	Output       string
	Error        error
}

// Queue manages the analysis worker pool
type Queue struct {
	db      *database.DB
	jobs    chan AnalysisJob
	results chan JobResult

	mu      sync.RWMutex
	pending map[int64]int      // submissionID -> pending job count
	total   map[int64]int      // submissionID -> total job count
	outputs map[int64][]string // submissionID -> collected outputs
}

var globalQueue *Queue

// InitQueue creates and starts the global queue with workers
func InitQueue(db *database.DB) {
	globalQueue = &Queue{
		db:      db,
		jobs:    make(chan AnalysisJob, 100),
		results: make(chan JobResult, 100),
		pending: make(map[int64]int),
		total:   make(map[int64]int),
		outputs: make(map[int64][]string),
	}

	// Start workers
	for i := 0; i < MaxWorkers; i++ {
		go globalQueue.worker(i)
	}

	// Start result collector
	go globalQueue.collector()

	log.Printf("Analysis queue initialized with %d workers", MaxWorkers)
}

// worker processes jobs from the queue
func (q *Queue) worker(id int) {
	for job := range q.jobs {
		log.Printf("Worker %d processing job [submission=%d, prompt=%s]",
			id, job.SubmissionID, job.PromptName)

		ag := agent.New()
		output, err := ag.Run(job.ContractFilePath, job.PromptText)

		q.results <- JobResult{
			SubmissionID: job.SubmissionID,
			PromptName:   job.PromptName,
			Output:       output,
			Error:        err,
		}
	}
}

// collector aggregates results and updates DB when all jobs for a submission complete
func (q *Queue) collector() {
	for result := range q.results {
		q.mu.Lock()

		// Log errors but don't include failed outputs
		if result.Error != nil {
			log.Printf("Analysis job failed [submission=%d, prompt=%s]: %v",
				result.SubmissionID, result.PromptName, result.Error)
		} else {
			log.Printf("Analysis job completed [submission=%d, prompt=%s]",
				result.SubmissionID, result.PromptName)
			q.outputs[result.SubmissionID] = append(
				q.outputs[result.SubmissionID], result.Output)
		}

		// Decrement pending count
		q.pending[result.SubmissionID]--

		// Check if all jobs for this submission are done
		if q.pending[result.SubmissionID] == 0 {
			var combined string

			if len(q.outputs[result.SubmissionID]) == 0 {
				// All jobs failed
				combined = "Analysis could not be completed."
				log.Printf("All analysis jobs failed for submission %d", result.SubmissionID)
			} else {
				// Concatenate successful outputs
				combined = strings.Join(q.outputs[result.SubmissionID], "\n\n---\n\n")
				log.Printf("Analysis complete for submission %d (%d results)",
					result.SubmissionID, len(q.outputs[result.SubmissionID]))
			}

			// Update database
			if err := q.db.UpdateSubmissionAnalysis(
				result.SubmissionID, combined, "complete"); err != nil {
				log.Printf("Failed to save analysis for submission %d: %v",
					result.SubmissionID, err)
			}

			// Cleanup
			delete(q.pending, result.SubmissionID)
			delete(q.total, result.SubmissionID)
			delete(q.outputs, result.SubmissionID)
		}

		q.mu.Unlock()
	}
}

// SubmitAnalysis enqueues analysis jobs for a submission
// Returns immediately (async processing)
func SubmitAnalysis(submissionID int64, contractFilePath string) error {
	prompts, err := DiscoverPrompts()
	if err != nil {
		return err
	}

	if len(prompts) == 0 {
		// No prompts configured - mark as complete with message
		log.Printf("No analysis prompts configured for submission %d", submissionID)
		return globalQueue.db.UpdateSubmissionAnalysis(submissionID,
			"No analysis prompts configured. Add prompt files to the prompts/ directory.", "complete")
	}

	globalQueue.mu.Lock()
	globalQueue.pending[submissionID] = len(prompts)
	globalQueue.total[submissionID] = len(prompts)
	globalQueue.outputs[submissionID] = make([]string, 0, len(prompts))
	globalQueue.mu.Unlock()

	log.Printf("Submitting %d analysis jobs for submission %d", len(prompts), submissionID)

	// Enqueue jobs
	for name, text := range prompts {
		globalQueue.jobs <- AnalysisJob{
			SubmissionID:     submissionID,
			ContractFilePath: contractFilePath,
			PromptName:       name,
			PromptText:       text,
		}
	}

	return nil
}

// GetProgress returns the analysis progress for a submission
// Returns (completed, total) counts
func GetProgress(submissionID int64) (completed, total int) {
	globalQueue.mu.RLock()
	defer globalQueue.mu.RUnlock()

	t, exists := globalQueue.total[submissionID]
	if !exists {
		return 0, 0
	}

	p := globalQueue.pending[submissionID]
	return t - p, t
}
