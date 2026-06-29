package sopbackend

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// UploadedBlobRef captures a stored file plus its metadata path.
type UploadedBlobRef struct {
	StoreName    string `json:"store_name"`
	FileName     string `json:"filename"`
	Path         string `json:"path"`
	MetadataPath string `json:"metadata_path"`
	Checksum     string `json:"checksum"`
	Size         int64  `json:"size"`
	ContentType  string `json:"content_type"`
	StoredAt     string `json:"stored_at"`
}

func getUploadedBlobRoot() string {
	for _, key := range []string{"SOP_BLOB_ROOT", "SOP_UPLOAD_ROOT"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return filepath.Join(os.TempDir(), "sop", "uploads")
}

func sanitizeStoreName(name string) string {
	clean := strings.TrimSpace(name)
	clean = strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(clean)
	clean = strings.Trim(clean, ".")
	if clean == "" {
		return "default"
	}
	return clean
}

func sanitizeFileName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "upload.bin"
	}
	base = strings.ReplaceAll(base, "/", "-")
	base = strings.ReplaceAll(base, "\\", "-")
	return base
}

func validateUploadedFileType(fileName, contentType string) error {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	allowedExtensions := map[string]bool{
		".pdf":      true,
		".txt":      true,
		".md":       true,
		".markdown": true,
		".html":     true,
		".htm":      true,
		".doc":      true,
		".docx":     true,
	}
	if !allowedExtensions[ext] {
		return fmt.Errorf("unsupported file type %q", fileName)
	}

	if strings.TrimSpace(contentType) == "" || strings.EqualFold(contentType, "application/octet-stream") {
		return nil
	}

	allowedTypes := map[string]bool{
		"application/pdf":    true,
		"text/plain":         true,
		"text/markdown":      true,
		"text/html":          true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-word": true,
		"application/zip":         true,
	}
	if allowedTypes[strings.ToLower(contentType)] {
		return nil
	}
	return nil
}

func stripHTMLTags(input string) string {
	var buf bytes.Buffer
	inTag := false
	for _, r := range input {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			buf.WriteRune(r)
		}
	}
	return strings.TrimSpace(buf.String())
}

func extractTextFromUploadedBlob(fileName string, data []byte, contentType string) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	trimmedType := strings.ToLower(strings.TrimSpace(contentType))
	if ext == ".txt" || ext == ".md" || ext == ".markdown" || ext == ".html" || ext == ".htm" || trimmedType == "text/plain" || trimmedType == "text/markdown" || trimmedType == "text/html" {
		text := string(data)
		if ext == ".html" || ext == ".htm" || trimmedType == "text/html" {
			text = stripHTMLTags(text)
		}
		return strings.TrimSpace(text), nil
	}

	if ext == ".docx" || trimmedType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("open docx archive: %w", err)
		}
		for _, f := range zr.File {
			if strings.ToLower(filepath.ToSlash(f.Name)) != "word/document.xml" {
				continue
			}
			reader, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open docx document: %w", err)
			}
			defer reader.Close()
			content, err := io.ReadAll(reader)
			if err != nil {
				return "", fmt.Errorf("read docx content: %w", err)
			}
			xml := string(content)
			xml = regexp.MustCompile(`<w:t[^>]*>(.*?)</w:t>`).ReplaceAllString(xml, "$1")
			xml = strings.ReplaceAll(xml, "<w:p>", "\n")
			xml = strings.ReplaceAll(xml, "</w:p>", "\n")
			xml = strings.ReplaceAll(xml, "<w:br/>", "\n")
			xml = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(xml, "")
			return strings.TrimSpace(xml), nil
		}
		return "", nil
	}

	if ext == ".pdf" || trimmedType == "application/pdf" {
		return "", fmt.Errorf("pdf extraction is not implemented yet")
	}

	return strings.TrimSpace(string(data)), nil
}

func persistUploadedBlob(storeName, fileName string, data []byte, contentType string) (UploadedBlobRef, error) {
	if len(data) == 0 {
		return UploadedBlobRef{}, fmt.Errorf("uploaded file is empty")
	}
	if err := validateUploadedFileType(fileName, contentType); err != nil {
		return UploadedBlobRef{}, err
	}

	checksumBytes := sha256.Sum256(data)
	checksum := hex.EncodeToString(checksumBytes[:])
	blobRoot := filepath.Join(getUploadedBlobRoot(), sanitizeStoreName(storeName), checksum[:2], checksum)
	if err := os.MkdirAll(blobRoot, 0o755); err != nil {
		return UploadedBlobRef{}, fmt.Errorf("create blob directory: %w", err)
	}

	storedName := sanitizeFileName(fileName)
	storedPath := filepath.Join(blobRoot, storedName)
	if err := os.WriteFile(storedPath, data, 0o644); err != nil {
		return UploadedBlobRef{}, fmt.Errorf("write uploaded blob: %w", err)
	}

	meta := map[string]any{
		"filename":      storedName,
		"original_name": fileName,
		"size":          len(data),
		"checksum":      checksum,
		"content_type":  contentType,
		"store":         sanitizeStoreName(storeName),
		"stored_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return UploadedBlobRef{}, fmt.Errorf("encode metadata: %w", err)
	}
	metaPath := filepath.Join(blobRoot, "meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
		return UploadedBlobRef{}, fmt.Errorf("write metadata: %w", err)
	}

	return UploadedBlobRef{
		StoreName:    sanitizeStoreName(storeName),
		FileName:     storedName,
		Path:         storedPath,
		MetadataPath: metaPath,
		Checksum:     checksum,
		Size:         int64(len(data)),
		ContentType:  contentType,
		StoredAt:     meta["stored_at"].(string),
	}, nil
}
