package sopbackend

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistUploadedBlobStoresFileAndMetadata(t *testing.T) {
	t.Setenv("SOP_BLOB_ROOT", t.TempDir())

	blob, err := persistUploadedBlob("demo-store", "sample.pdf", []byte("hello world"), "application/pdf")
	if err != nil {
		t.Fatalf("persistUploadedBlob returned error: %v", err)
	}

	if !strings.HasSuffix(blob.Path, filepath.Join("sample.pdf")) {
		t.Fatalf("expected stored file path to end with sample.pdf, got %q", blob.Path)
	}

	if _, err := os.Stat(blob.Path); err != nil {
		t.Fatalf("expected stored file to exist: %v", err)
	}

	metaPath := filepath.Join(filepath.Dir(blob.Path), "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("expected metadata file to exist: %v", err)
	}

	var meta struct {
		FileName string `json:"filename"`
		Size     int    `json:"size"`
		Checksum string `json:"checksum"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}

	if meta.FileName != "sample.pdf" {
		t.Fatalf("expected metadata filename sample.pdf, got %q", meta.FileName)
	}
	if meta.Size != len("hello world") {
		t.Fatalf("expected metadata size %d, got %d", len("hello world"), meta.Size)
	}
	if meta.Checksum == "" {
		t.Fatal("expected checksum to be populated")
	}
}

func TestValidateUploadedFileTypeRejectsUnsupportedFiles(t *testing.T) {
	if err := validateUploadedFileType("notes.exe", "application/x-msdownload"); err == nil {
		t.Fatal("expected unsupported file type to be rejected")
	}

	if err := validateUploadedFileType("notes.pdf", "application/pdf"); err != nil {
		t.Fatalf("expected supported pdf to pass validation: %v", err)
	}

	if err := validateUploadedFileType("notes.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"); err != nil {
		t.Fatalf("expected supported docx to pass validation: %v", err)
	}
}

func TestExtractTextFromUploadedBlobSupportsTextAndDocx(t *testing.T) {
	text, err := extractTextFromUploadedBlob("notes.txt", []byte("hello world\nnext line"), "text/plain")
	if err != nil {
		t.Fatalf("expected txt extraction to succeed: %v", err)
	}
	if !strings.Contains(text, "hello world") {
		t.Fatalf("expected extracted text to contain source content, got %q", text)
	}

	var docxBuf bytes.Buffer
	zw := zip.NewWriter(&docxBuf)
	f, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := f.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Docx hello</w:t></w:r></w:p></w:body></w:document>`)); err != nil {
		t.Fatalf("failed to write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	docxText, err := extractTextFromUploadedBlob("notes.docx", docxBuf.Bytes(), "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err != nil {
		t.Fatalf("expected docx extraction to succeed: %v", err)
	}
	if !strings.Contains(docxText, "Docx hello") {
		t.Fatalf("expected extracted docx text to contain content, got %q", docxText)
	}
}
