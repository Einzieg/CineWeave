package media

import (
	"context"

	"github.com/Einzieg/cineweave/internal/storage"
)

type Clip struct {
	ShotID                string        `json:"shotId"`
	ShotIndex             int           `json:"shotIndex"`
	StorageKey            string        `json:"storageKey"`
	MimeType              string        `json:"mimeType"`
	DurationSeconds       float64       `json:"durationSeconds,omitempty"`
	TrimStartSeconds      float64       `json:"trimStartSeconds,omitempty"`
	TrimEndSeconds        *float64      `json:"trimEndSeconds,omitempty"`
	TargetDurationSeconds *float64      `json:"targetDurationSeconds,omitempty"`
	TextOverlays          []TextOverlay `json:"textOverlays,omitempty"`
}

type TextOverlay struct {
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	Position     string  `json:"position,omitempty"`
}

type EndCard struct {
	Text            string  `json:"text"`
	DurationSeconds float64 `json:"durationSeconds"`
}

type ComposeRequest struct {
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	WorkflowRunID  string `json:"workflowRunId"`

	Clips            []Clip   `json:"clips"`
	EndCard          *EndCard `json:"endCard,omitempty"`
	AspectRatio      string   `json:"aspectRatio"`
	Resolution       string   `json:"resolution"`
	FPSNumerator     int      `json:"fpsNumerator,omitempty"`
	FPSDenominator   int      `json:"fpsDenominator,omitempty"`
	OutputMimeType   string   `json:"outputMimeType"`
	OutputStorageKey string   `json:"outputStorageKey,omitempty"`
}

type ComposeResult struct {
	StorageKey      string      `json:"storageKey"`
	MimeType        string      `json:"mimeType"`
	ByteSize        int64       `json:"byteSize"`
	ContentHash     string      `json:"contentHash"`
	DurationSeconds float64     `json:"durationSeconds,omitempty"`
	Width           int         `json:"width,omitempty"`
	Height          int         `json:"height,omitempty"`
	Probe           ProbeResult `json:"probe"`
}

type ObjectStore interface {
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error)
	PutFile(ctx context.Context, key, filePath, contentType string) (storage.PutResult, error)
}
