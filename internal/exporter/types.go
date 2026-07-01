package exporter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxExportObjectBytes int64 = 512 << 20

type ObjectStore interface {
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error)
	PutFile(ctx context.Context, key, filePath, contentType string) (storage.PutResult, error)
}

type Exporter struct {
	db      *pgxpool.Pool
	storage ObjectStore
	now     func() time.Time
}

type Request struct {
	OrganizationID string         `json:"organizationId"`
	ProjectID      string         `json:"projectId"`
	WorkflowRunID  string         `json:"workflowRunId"`
	ExportID       string         `json:"exportId"`
	ExportType     string         `json:"exportType"`
	Format         string         `json:"format"`
	Title          string         `json:"title"`
	Options        map[string]any `json:"options"`
	CreatedBy      string         `json:"createdBy"`
}

type Result struct {
	ExportID    string         `json:"exportId"`
	ExportType  string         `json:"exportType"`
	Format      string         `json:"format"`
	StorageKey  string         `json:"storageKey"`
	ArtifactID  string         `json:"artifactId,omitempty"`
	MediaFileID string         `json:"mediaFileId,omitempty"`
	ByteSize    int64          `json:"byteSize,omitempty"`
	ContentHash string         `json:"contentHash,omitempty"`
	MimeType    string         `json:"mimeType"`
	Output      map[string]any `json:"output"`
}

type ProjectSnapshot struct {
	ExportedAt             string           `json:"exportedAt"`
	Project                json.RawMessage  `json:"project"`
	Sources                json.RawMessage  `json:"sources"`
	Events                 json.RawMessage  `json:"events"`
	AdaptationPlans        json.RawMessage  `json:"adaptationPlans"`
	Scripts                json.RawMessage  `json:"scripts"`
	ScriptScenes           json.RawMessage  `json:"scriptScenes"`
	Assets                 json.RawMessage  `json:"assets"`
	StoryboardShots        json.RawMessage  `json:"storyboardShots"`
	ShotAssetRequirements  json.RawMessage  `json:"shotAssetRequirements"`
	Timelines              json.RawMessage  `json:"timelines"`
	TimelineClips          json.RawMessage  `json:"timelineClips"`
	FinalVideos            json.RawMessage  `json:"finalVideos"`
	IncludedStorageObjects []ExportedObject `json:"includedStorageObjects,omitempty"`
	SkippedStorageObjects  []SkippedObject  `json:"skippedStorageObjects,omitempty"`
}

type ExportedObject struct {
	StorageKey  string `json:"storageKey"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	ArtifactID  string `json:"artifactId,omitempty"`
	MediaFileID string `json:"mediaFileId,omitempty"`
	ShotID      string `json:"shotId,omitempty"`
	AssetID     string `json:"assetId,omitempty"`
}

type SkippedObject struct {
	StorageKey string `json:"storageKey"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Reason     string `json:"reason"`
}

func New(db *pgxpool.Pool, objectStore ObjectStore) *Exporter {
	return &Exporter{db: db, storage: objectStore, now: func() time.Time { return time.Now().UTC() }}
}
