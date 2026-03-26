package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"contractanalyzer/models"

	_ "modernc.org/sqlite"
)

// DB wraps the sql.DB connection
type DB struct {
	*sql.DB
}

// Open creates a new database connection
func Open(dataSourceName string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dataSourceName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, err
	}
	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

// Migrate creates tables if they don't exist
func (db *DB) Migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS submissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		original_filename TEXT,
		contract_file TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		analysis_result TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_submissions_created_at ON submissions(created_at DESC);
	`
	_, err := db.Exec(query)
	return err
}

// CreateSubmission inserts a new submission and returns its ID
func (db *DB) CreateSubmission(s *models.Submission) (int64, error) {
	now := time.Now()
	result, err := db.Exec(`
		INSERT INTO submissions (title, original_filename, contract_file, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, s.Title, s.OriginalFilename, s.ContractFile, s.Status, now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetSubmission retrieves a submission by ID
func (db *DB) GetSubmission(id int64) (*models.Submission, error) {
	s := &models.Submission{}
	var originalFilename, analysisResult sql.NullString
	err := db.QueryRow(`
		SELECT id, title, original_filename, contract_file, status, analysis_result, created_at, updated_at
		FROM submissions WHERE id = ?
	`, id).Scan(&s.ID, &s.Title, &originalFilename, &s.ContractFile, &s.Status, &analysisResult, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.OriginalFilename = originalFilename
	s.AnalysisResult = analysisResult
	return s, nil
}

// GetAllSubmissions retrieves all submissions ordered by created_at desc
func (db *DB) GetAllSubmissions() ([]*models.Submission, error) {
	rows, err := db.Query(`
		SELECT id, title, original_filename, contract_file, status, analysis_result, created_at, updated_at
		FROM submissions ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var submissions []*models.Submission
	for rows.Next() {
		s := &models.Submission{}
		var originalFilename, analysisResult sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &originalFilename, &s.ContractFile, &s.Status, &analysisResult, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.OriginalFilename = originalFilename
		s.AnalysisResult = analysisResult
		submissions = append(submissions, s)
	}
	return submissions, nil
}

// UpdateSubmissionFile updates the contract file path
func (db *DB) UpdateSubmissionFile(id int64, contractFile string) error {
	_, err := db.Exec(`
		UPDATE submissions SET contract_file = ?, updated_at = ? WHERE id = ?
	`, contractFile, time.Now(), id)
	return err
}

// UpdateSubmissionAnalysis updates the analysis result and status
func (db *DB) UpdateSubmissionAnalysis(id int64, analysis string, status string) error {
	_, err := db.Exec(`
		UPDATE submissions SET analysis_result = ?, status = ?, updated_at = ? WHERE id = ?
	`, analysis, status, time.Now(), id)
	return err
}

// UpdateSubmissionStatus updates only the status field
func (db *DB) UpdateSubmissionStatus(id int64, status string) error {
	_, err := db.Exec(`
		UPDATE submissions SET status = ?, updated_at = ? WHERE id = ?
	`, status, time.Now(), id)
	return err
}
