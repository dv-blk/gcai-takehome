package models

import (
	"database/sql"
	"time"
)

// Submission represents a contract submission for analysis
type Submission struct {
	ID               int64          `json:"id"`
	Title            string         `json:"title"`
	OriginalFilename sql.NullString `json:"original_filename,omitempty"` // PDF filename if uploaded
	ContractFile     string         `json:"contract_file"`               // stored .txt filename
	Status           string         `json:"status"`                      // "pending", "analyzing", "complete"
	AnalysisResult   sql.NullString `json:"analysis_result,omitempty"`   // markdown analysis
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`

	// Transient field - not stored in DB, loaded from file on demand
	ContractText string `json:"-"`
}

// HasOriginalFilename returns true if a PDF was uploaded
func (s *Submission) HasOriginalFilename() bool {
	return s.OriginalFilename.Valid && s.OriginalFilename.String != ""
}

// GetOriginalFilename returns the filename or empty string
func (s *Submission) GetOriginalFilename() string {
	if s.OriginalFilename.Valid {
		return s.OriginalFilename.String
	}
	return ""
}

// HasAnalysisResult returns true if analysis is complete
func (s *Submission) HasAnalysisResult() bool {
	return s.AnalysisResult.Valid && s.AnalysisResult.String != ""
}

// GetAnalysisResult returns the analysis or empty string
func (s *Submission) GetAnalysisResult() string {
	if s.AnalysisResult.Valid {
		return s.AnalysisResult.String
	}
	return ""
}
