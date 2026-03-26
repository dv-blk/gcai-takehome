package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"

	"contractanalyzer/analyzer"
	"contractanalyzer/database"
	"contractanalyzer/models"
	"contractanalyzer/pdfutil"
	"contractanalyzer/storage"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	tmpl *template.Template
	db   *database.DB
}

// New creates a new Handler instance
func New(tmpl *template.Template, db *database.DB) *Handler {
	return &Handler{
		tmpl: tmpl,
		db:   db,
	}
}

// PageData represents data passed to the main page template
type PageData struct {
	Submissions      []*models.Submission
	ActiveSubmission *models.Submission
	ActiveID         int64
	ShowForm         bool
	Error            string
}

// Index renders the main page - handles both form display and submission viewing via ?id=
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	submissions, err := h.db.GetAllSubmissions()
	if err != nil {
		log.Printf("Error getting submissions: %v", err)
		submissions = []*models.Submission{}
	}

	var data PageData
	data.Submissions = submissions

	idStr := r.URL.Query().Get("id")

	if idStr != "" {
		// Load specific submission
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			submission, err := h.db.GetSubmission(id)
			if err == nil && submission != nil {
				// Load contract text from file
				contractText, err := storage.LoadContract(submission.ContractFile)
				if err != nil {
					log.Printf("Error loading contract file: %v", err)
					contractText = "(Error loading contract text)"
				}
				submission.ContractText = contractText

				// Load analysis progress if still analyzing
				if submission.Status == "analyzing" {
					completed, total := analyzer.GetProgress(submission.ID)
					submission.AnalysisProgress = completed
					submission.AnalysisTotal = total
					if total > 0 {
						submission.AnalysisPercent = (completed * 100) / total
					}
				}

				data.ActiveSubmission = submission
				data.ActiveID = submission.ID
				data.ShowForm = false
			} else {
				// Submission not found, show form
				data.ShowForm = true
			}
		} else {
			data.ShowForm = true
		}
	} else {
		data.ShowForm = true
	}

	// Check if this is an HTMX request
	if r.Header.Get("HX-Request") == "true" {
		if data.ShowForm {
			h.tmpl.ExecuteTemplate(w, "submission_form.html", nil)
			// Also update sidebar to clear active state
			h.renderSidebarOOB(w, 0)
		} else {
			h.tmpl.ExecuteTemplate(w, "submission_view.html", data.ActiveSubmission)
			// Also update sidebar to highlight active submission
			h.renderSidebarOOB(w, data.ActiveID)
		}
		return
	}

	// Full page render
	if err := h.tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// NewSubmissionForm renders the new submission form partial
func (h *Handler) NewSubmissionForm(w http.ResponseWriter, r *http.Request) {
	// Set header to update URL when navigating to new form
	w.Header().Set("HX-Push-Url", "/")
	if err := h.tmpl.ExecuteTemplate(w, "submission_form.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Clear sidebar highlight
	h.renderSidebarOOB(w, 0)
}

// CreateSubmission handles form submission and returns analysis results
func (h *Handler) CreateSubmission(w http.ResponseWriter, r *http.Request) {
	// Parse form data
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
		h.renderError(w, "Error parsing form: "+err.Error())
		return
	}

	title := r.FormValue("title")
	contractText := r.FormValue("contract_text")
	var originalFilename string

	// Check for PDF upload
	file, header, err := r.FormFile("pdf_file")
	if err == nil && file != nil {
		defer file.Close()
		originalFilename = header.Filename

		// Read PDF content
		pdfData, err := io.ReadAll(file)
		if err != nil {
			h.renderError(w, "Error reading PDF file: "+err.Error())
			return
		}

		// Extract text from PDF
		extractedText, err := pdfutil.ExtractTextFromBytes(pdfData)
		if err != nil {
			h.renderError(w, "Error extracting text from PDF: "+err.Error())
			return
		}

		if extractedText == "" {
			h.renderError(w, "Could not extract text from PDF. The PDF may be image-based or corrupted.")
			return
		}

		contractText = extractedText
	}

	if contractText == "" {
		h.renderError(w, "No contract text provided. Please paste text or upload a PDF.")
		return
	}

	if title == "" {
		title = "Untitled Contract"
	}

	// Create submission in DB
	submission := &models.Submission{
		Title:            title,
		OriginalFilename: sql.NullString{String: originalFilename, Valid: originalFilename != ""},
		ContractFile:     "", // Will be set after file save
		Status:           "pending",
	}

	id, err := h.db.CreateSubmission(submission)
	if err != nil {
		h.renderError(w, "Error saving submission: "+err.Error())
		return
	}

	// Save contract text to file
	filename, err := storage.SaveContract(id, contractText)
	if err != nil {
		h.renderError(w, "Error saving contract file: "+err.Error())
		return
	}

	// Update submission with filename
	if err := h.db.UpdateSubmissionFile(id, filename); err != nil {
		h.renderError(w, "Error updating submission: "+err.Error())
		return
	}

	// Set status to analyzing and submit to background queue
	if err := h.db.UpdateSubmissionStatus(id, "analyzing"); err != nil {
		h.renderError(w, "Error updating submission status: "+err.Error())
		return
	}

	// Submit analysis jobs to the queue (async - returns immediately)
	contractFullPath := filepath.Join(storage.ContractsDir, filename)
	if err := analyzer.SubmitAnalysis(id, contractFullPath); err != nil {
		log.Printf("Failed to submit analysis for submission %d: %v", id, err)
		// Mark as complete with error message
		h.db.UpdateSubmissionAnalysis(id, "Analysis submission failed: "+err.Error(), "complete")
	}

	// Set HTMX headers for URL update
	w.Header().Set("HX-Push-Url", "/?id="+strconv.FormatInt(id, 10))

	// Load the submission to render the view (will show analyzing status with polling)
	submission, err = h.db.GetSubmission(id)
	if err != nil {
		h.renderError(w, "Error loading submission: "+err.Error())
		return
	}
	submission.ContractText = contractText

	// Get initial progress
	completed, total := analyzer.GetProgress(id)
	submission.AnalysisProgress = completed
	submission.AnalysisTotal = total
	if total > 0 {
		submission.AnalysisPercent = (completed * 100) / total
	}

	// Render the submission view (will include polling for analyzing status)
	if err := h.tmpl.ExecuteTemplate(w, "submission_view.html", submission); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Render the sidebar with OOB swap to update the submissions list
	if err := h.renderSidebarOOB(w, id); err != nil {
		log.Printf("Error rendering sidebar OOB: %v", err)
	}
}

// renderError renders an error message to the user
func (h *Handler) renderError(w http.ResponseWriter, message string) {
	data := struct{ Error string }{Error: message}
	if err := h.tmpl.ExecuteTemplate(w, "error.html", data); err != nil {
		http.Error(w, message, http.StatusInternalServerError)
	}
}

// renderSidebarOOB renders the sidebar partial with hx-swap-oob for out-of-band updates
func (h *Handler) renderSidebarOOB(w http.ResponseWriter, activeID int64) error {
	submissions, err := h.db.GetAllSubmissions()
	if err != nil {
		return err
	}

	// Write the OOB swap wrapper
	w.Write([]byte(`<nav id="submissions-list" hx-swap-oob="true" class="space-y-2">`))

	// Render each submission item
	for _, sub := range submissions {
		statusBadge := ""
		switch sub.Status {
		case "complete":
			statusBadge = `<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400">Complete</span>`
		case "analyzing":
			statusBadge = `<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400">Analyzing</span>`
		default:
			statusBadge = `<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300">Pending</span>`
		}

		// Highlight active submission
		activeClass := ""
		if sub.ID == activeID {
			activeClass = "bg-accent/10 border-accent/30 dark:border-accent/50"
		}

		fmt.Fprintf(w, `<button 
			hx-get="/?id=%d"
			hx-target="#main-content"
			hx-swap="innerHTML"
			hx-push-url="/?id=%d"
			class="w-full text-left p-3 rounded-md hover:bg-bg dark:hover:bg-dark-bg border border-transparent hover:border-bg-muted/30 dark:hover:border-gray-600 transition-colors group %s"
		>
			<div class="flex items-start justify-between">
				<div class="flex-1 min-w-0">
					<p class="text-sm font-medium text-text dark:text-white truncate">%s</p>
					<p class="text-xs text-text-subtle dark:text-gray-400 mt-1">%s</p>
				</div>
				<span class="ml-2 flex-shrink-0">%s</span>
			</div>
		</button>`, sub.ID, sub.ID, activeClass, template.HTMLEscapeString(sub.Title), sub.CreatedAt.Format("Jan 2, 2006"), statusBadge)
	}

	if len(submissions) == 0 {
		w.Write([]byte(`<p class="text-sm text-text-subtle dark:text-gray-400 text-center py-4">No submissions yet</p>`))
	}

	w.Write([]byte(`</nav>`))
	return nil
}
