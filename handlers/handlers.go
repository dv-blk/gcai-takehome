package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"

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

// AnalysisResultData represents data passed to the analysis result template
type AnalysisResultData struct {
	Title            string
	ContractText     string
	AnalysisMarkdown template.JS
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

	// Generate analysis (example markdown for now)
	analysisMarkdown := getExampleAnalysis()

	// Update submission with analysis result
	if err := h.db.UpdateSubmissionAnalysis(id, analysisMarkdown, "complete"); err != nil {
		h.renderError(w, "Error updating analysis: "+err.Error())
		return
	}

	// Set HTMX headers for URL update
	w.Header().Set("HX-Push-Url", "/?id="+strconv.FormatInt(id, 10))

	// Render the analysis result
	data := AnalysisResultData{
		Title:            title,
		ContractText:     contractText,
		AnalysisMarkdown: template.JS(analysisMarkdown),
	}

	if err := h.tmpl.ExecuteTemplate(w, "analysis_result.html", data); err != nil {
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

// getExampleAnalysis returns example markdown analysis
func getExampleAnalysis() string {
	return `---

**Clause:**  
"Provider reserves the right to modify pricing at any time with seven (7) days written notice."

**Issue:**  
Unilateral price increases with minimal notice period.

**Risk:**  
Budget unpredictability; vendor could significantly increase costs mid-contract with insufficient time to evaluate alternatives or negotiate.

**Fix:**  
Require 90-day notice for price changes; cap annual increases at CPI + 3%; or lock pricing for the initial term with agreed escalation schedule.

**Confidence:**  
High

---

**Clause:**  
"All fees are non-refundable, including unused portions of prepaid subscriptions."

**Issue:**  
No refund rights regardless of service quality or early termination.

**Risk:**  
Financial loss if service is unsatisfactory or business needs change; no leverage for service issues.

**Fix:**  
Negotiate pro-rata refunds for unused service periods; add termination for cause with refund rights.

**Confidence:**  
High

---

**Clause:**  
"This Agreement shall automatically renew for successive two (2) year terms unless Customer provides written notice at least ninety (90) days prior."

**Issue:**  
Long auto-renewal period with early cancellation deadline.

**Risk:**  
Easy to miss the 90-day window, resulting in unwanted 2-year commitments and budget lock-in.

**Fix:**  
Reduce to 1-year auto-renewal with 30-day notice requirement; request renewal reminder from vendor; or require opt-in renewal.

**Confidence:**  
High

---

**Clause:**  
"Customer grants Provider a perpetual, irrevocable, royalty-free license to use Customer Data in anonymized form for any purpose, including commercial sale."

**Issue:**  
Overly broad data rights that survive termination and allow commercial exploitation.

**Risk:**  
Competitive intelligence leakage; customer data could benefit competitors; regulatory concerns with "anonymized" data that may be re-identifiable.

**Fix:**  
Limit to aggregate statistics only; prohibit sale to third parties; add deletion requirement upon termination; require explicit consent for any commercial use.

**Confidence:**  
High

---

**Clause:**  
"PROVIDER'S TOTAL LIABILITY SHALL NOT EXCEED THE FEES PAID IN THE THREE (3) MONTHS PRECEDING THE CLAIM."

**Issue:**  
Severely limited liability cap.

**Risk:**  
Inadequate recovery for significant damages; 3-month cap may be trivial compared to actual losses from data breach or service failure.

**Fix:**  
Negotiate 12-month fee cap minimum; carve out gross negligence, willful misconduct, and data breaches; consider requiring cyber insurance.

**Confidence:**  
High

---

**Clause:**  
"Customer shall indemnify Provider against any and all claims arising from Customer's use of the Service."

**Issue:**  
One-sided indemnification favoring vendor.

**Risk:**  
Customer bears all legal defense costs even for issues caused by the service; unlimited exposure.

**Fix:**  
Make indemnification mutual; limit to claims arising from actual breach or negligence; cap indemnification liability.

**Confidence:**  
Medium

---

**Clause:**  
"Provider may audit Customer's use of the Service upon twenty-four (24) hours notice."

**Issue:**  
Minimal notice for potentially disruptive audits.

**Risk:**  
Business disruption; potential access to sensitive systems with little preparation time.

**Fix:**  
Require 30-day notice for routine audits; limit audit scope and frequency (e.g., once per year); require confidentiality of audit findings.

**Confidence:**  
Medium

---

**Clause:**  
"Customer waives any right to participate in class action lawsuits against Provider."

**Issue:**  
Class action waiver limits collective legal remedies.

**Risk:**  
Reduced leverage for small but widespread issues; forces individual arbitration which favors well-resourced vendors.

**Fix:**  
Strike this clause if possible; alternatively, ensure individual arbitration is fair and accessible.

**Confidence:**  
Medium`
}
