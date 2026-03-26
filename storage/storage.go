package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// DataDir is the root data directory
	DataDir = "data"
	// ContractsDir is where contract text files are stored
	ContractsDir = "data/contracts"
)

// Init creates the data directories if they don't exist
func Init() error {
	return os.MkdirAll(ContractsDir, 0755)
}

// GenerateFilename creates a unique filename for a contract
func GenerateFilename(id int64) string {
	return fmt.Sprintf("contract_%d_%d.txt", id, time.Now().Unix())
}

// SaveContract writes contract text to a file and returns the filename
func SaveContract(id int64, content string) (string, error) {
	filename := GenerateFilename(id)
	fullPath := filepath.Join(ContractsDir, filename)

	err := os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return filename, nil
}

// LoadContract reads contract text from a file
func LoadContract(filename string) (string, error) {
	fullPath := filepath.Join(ContractsDir, filename)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
