package topology

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type BundleOptions struct {
	ConfigPath  string
	StoragePath string
	OutputPath  string
	Name        string
	Version     string
}

type BundleManifest struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Config    string `json:"config"`
	Storage   string `json:"storage"`
	CreatedBy string `json:"created_by,omitempty"`
}

func CreateBundle(opts BundleOptions) (BundleManifest, error) {
	if strings.TrimSpace(opts.ConfigPath) == "" || strings.TrimSpace(opts.OutputPath) == "" {
		return BundleManifest{}, fmt.Errorf("config path and output path are required")
	}
	if strings.TrimSpace(opts.Name) == "" {
		opts.Name = "local-rag"
	}
	if strings.TrimSpace(opts.Version) == "" {
		opts.Version = "1.0.0"
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	defer zw.Close()

	if err := addFileToZip(zw, opts.ConfigPath, "config.json"); err != nil {
		return BundleManifest{}, err
	}
	if strings.TrimSpace(opts.StoragePath) != "" {
		if err := addDirToZip(zw, opts.StoragePath, "storage"); err != nil {
			return BundleManifest{}, err
		}
	}

	manifest := BundleManifest{
		Name:    opts.Name,
		Version: opts.Version,
		Config:  "config.json",
		Storage: "storage",
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BundleManifest{}, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeZipFile(zw, "manifest.json", manifestBytes); err != nil {
		return BundleManifest{}, err
	}
	if err := zw.Close(); err != nil {
		return BundleManifest{}, fmt.Errorf("close zip: %w", err)
	}

	if err := os.WriteFile(opts.OutputPath, buf.Bytes(), 0o644); err != nil {
		return BundleManifest{}, fmt.Errorf("write bundle: %w", err)
	}
	return manifest, nil
}

func addFileToZip(zw *zip.Writer, srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	return writeZipFile(zw, dstPath, data)
}

func addDirToZip(zw *zip.Writer, srcDir, dstRoot string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		dstPath := filepath.ToSlash(filepath.Join(dstRoot, relPath))
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeZipFile(zw, dstPath, data)
	})
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", name, err)
	}
	_, err = io.Copy(w, bytes.NewReader(data))
	return err
}
