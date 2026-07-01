package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func SafeFileName(value, fallback string) string {
	name := strings.TrimSpace(value)
	if name == "" {
		name = fallback
	}
	var builder strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			builder.WriteRune('_')
		default:
			if r < 32 {
				builder.WriteRune('_')
			} else {
				builder.WriteRune(r)
			}
		}
	}
	name = strings.TrimSpace(builder.String())
	name = strings.Trim(name, ". ")
	if name == "" {
		name = fallback
	}
	if utf8.RuneCountInString(name) > 96 {
		runes := []rune(name)
		name = string(runes[:96])
	}
	return name
}

func safeZipPath(parts ...string) (string, error) {
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if path.IsAbs(part) || strings.Contains(part, `\`) {
			return "", fmt.Errorf("invalid zip path %q", part)
		}
		for _, segment := range strings.Split(part, "/") {
			if segment == ".." {
				return "", fmt.Errorf("invalid zip path %q", part)
			}
		}
		cleanParts = append(cleanParts, part)
	}
	cleaned := path.Clean(path.Join(cleanParts...))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, `\`) {
		return "", fmt.Errorf("invalid zip path %q", cleaned)
	}
	return cleaned, nil
}

func addZipBytes(writer *zip.Writer, name string, body []byte) error {
	cleaned, err := safeZipPath(name)
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: cleaned, Method: zip.Deflate}
	file, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, bytes.NewReader(body))
	return err
}

func addZipJSON(writer *zip.Writer, name string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return addZipBytes(writer, name, body)
}

func writeTempFile(prefix string, body []byte) (string, func(), error) {
	file, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return file.Name(), cleanup, nil
}

func createTempZip(prefix string, build func(*zip.Writer) error) (string, func(), error) {
	file, err := os.CreateTemp("", prefix+"-*.zip")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	writer := zip.NewWriter(file)
	if err := build(writer); err != nil {
		_ = writer.Close()
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return file.Name(), cleanup, nil
}

func storageExtension(storageKey, contentType, fallback string) string {
	ext := strings.ToLower(filepath.Ext(storageKey))
	if ext != "" && len(ext) <= 12 {
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "application/json":
		return ".json"
	case "text/markdown":
		return ".md"
	default:
		return fallback
	}
}

func (e *Exporter) addStorageObject(ctx context.Context, writer *zip.Writer, meta *ProjectSnapshot, object ExportedObject) {
	if e.storage == nil || strings.TrimSpace(object.StorageKey) == "" {
		meta.SkippedStorageObjects = append(meta.SkippedStorageObjects, SkippedObject{StorageKey: object.StorageKey, Path: object.Path, Type: object.Type, Reason: "storage object is not available"})
		return
	}
	body, _, err := e.storage.GetObject(ctx, object.StorageKey, MaxExportObjectBytes)
	if err != nil {
		meta.SkippedStorageObjects = append(meta.SkippedStorageObjects, SkippedObject{StorageKey: object.StorageKey, Path: object.Path, Type: object.Type, Reason: err.Error()})
		return
	}
	if err := addZipBytes(writer, object.Path, body); err != nil {
		meta.SkippedStorageObjects = append(meta.SkippedStorageObjects, SkippedObject{StorageKey: object.StorageKey, Path: object.Path, Type: object.Type, Reason: err.Error()})
		return
	}
	meta.IncludedStorageObjects = append(meta.IncludedStorageObjects, object)
}
