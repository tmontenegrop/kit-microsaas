package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const MaxUploadSize = 10 << 20 // 10 MB

var allowedMIMETypes = map[string]bool{
	"application/pdf":       true,
	"image/jpeg":            true,
	"image/png":             true,
	"image/gif":             true,
	"image/webp":            true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/json": true,
	"text/csv":         true,
	"text/plain":       true,
}

type Local struct {
	BasePath string
}

func NewLocal(basePath string) (*Local, error) {
	abs, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("storage path: %w", err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &Local{BasePath: abs}, nil
}

func (s *Local) resolve(relativePath string) (string, error) {
	fullPath := filepath.Join(s.BasePath, relativePath)
	abs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("storage path: %w", err)
	}
	rel, err := filepath.Rel(s.BasePath, abs)
	if err != nil {
		return "", fmt.Errorf("storage path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("acceso denegado")
	}
	return abs, nil
}

func (s *Local) Save(subdir, filename string, data io.Reader) (string, error) {
	return s.saveWithValidation(subdir, filename, data, 0, nil)
}

func (s *Local) SaveWithValidation(subdir, filename string, data io.Reader, maxSize int64, allowedMIME map[string]bool) (string, error) {
	return s.saveWithValidation(subdir, filename, data, maxSize, allowedMIME)
}

func (s *Local) saveWithValidation(subdir, filename string, data io.Reader, maxSize int64, allowedMIME map[string]bool) (string, error) {
	if maxSize <= 0 {
		maxSize = MaxUploadSize
	}
	if allowedMIME == nil {
		allowedMIME = allowedMIMETypes
	}

	limited := io.LimitReader(data, maxSize+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("leer archivo: %w", err)
	}
	if int64(len(buf)) > maxSize {
		return "", fmt.Errorf("archivo excede el tamaño maximo de %d bytes", maxSize)
	}

	mime := http.DetectContentType(buf)
	if !allowedMIME[mime] {
		return "", fmt.Errorf("tipo de archivo no permitido: %s", mime)
	}

	safeFilename := filepath.Base(filename)
	abs, err := s.resolve(filepath.Join(subdir, safeFilename))
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create subdir: %w", err)
	}

	if err := os.WriteFile(abs, buf, 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return filepath.ToSlash(filepath.Join(subdir, safeFilename)), nil
}

func (s *Local) Open(relativePath string) (io.ReadCloser, error) {
	abs, err := s.resolve(relativePath)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

func (s *Local) Delete(relativePath string) error {
	abs, err := s.resolve(relativePath)
	if err != nil {
		return err
	}

	return os.Remove(abs)
}

func (s *Local) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relativePath := strings.TrimPrefix(r.URL.Path, "/storage/")
		if relativePath == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		relativePath = filepath.ToSlash(relativePath)

		f, err := s.Open(relativePath)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer func() { _ = f.Close() }()

		ext := filepath.Ext(relativePath)
		switch strings.ToLower(ext) {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".gif":
			w.Header().Set("Content-Type", "image/gif")
		case ".webp":
			w.Header().Set("Content-Type", "image/webp")
		case ".pdf":
			w.Header().Set("Content-Type", "application/pdf")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		_, _ = io.Copy(w, f)
	})
}
