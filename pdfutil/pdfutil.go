package pdfutil

import (
	"bytes"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractTextFromBytes extracts text content from PDF bytes
func ExtractTextFromBytes(data []byte) (string, error) {
	reader := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(reader, int64(len(data)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	numPages := pdfReader.NumPage()

	for i := 1; i <= numPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue // Skip pages that fail to parse
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}

	return strings.TrimSpace(buf.String()), nil
}
