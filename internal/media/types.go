package media

import (
	"context"

	"github.com/Einzieg/cineweave/internal/storage"
)

type Clip struct {
	ShotID          string  `json:"shotId"`
	ShotIndex       int     `json:"shotIndex"`
	StorageKey      string  `json:"storageKey"`
	MimeType        string  `json:"mimeType"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
}

type ComposeRequest struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`

	Clips          []Clip `json:"clips"`
	AspectRatio    string `json:"aspectRatio"`
	Resolution     string `json:"resolution"`
	OutputMimeType string `json:"outputMimeType"`
}

type ComposeResult struct {
	StorageKey      string  `json:"storageKey"`
	MimeType        string  `json:"mimeType"`
	ByteSize        int64   `json:"byteSize"`
	ContentHash     string  `json:"contentHash"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
}

type ObjectStore interface {
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error)
	PutFile(ctx context.Context, key, filePath, contentType string) (storage.PutResult, error)
}
