package analyzer

import (
	"os"
	"path/filepath"
	"strings"
)

// PromptsDir is the directory containing prompt files
const PromptsDir = "prompts"

// DiscoverPrompts reads all .txt files from the prompts directory
// Returns a map of prompt name (filename without extension) -> content
func DiscoverPrompts() (map[string]string, error) {
	prompts := make(map[string]string)

	entries, err := os.ReadDir(PromptsDir)
	if os.IsNotExist(err) {
		// Create directory if it doesn't exist
		if mkErr := os.MkdirAll(PromptsDir, 0755); mkErr != nil {
			return nil, mkErr
		}
		return prompts, nil
	}
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(PromptsDir, entry.Name()))
		if err != nil {
			continue // Skip unreadable files
		}

		name := strings.TrimSuffix(entry.Name(), ".txt")
		prompts[name] = string(content)
	}

	return prompts, nil
}
