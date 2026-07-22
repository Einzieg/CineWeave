package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Einzieg/cineweave/internal/storage"
)

const DefaultMaxStoryboardSheetBytes int64 = 64 << 20

type StoryboardSheetCropRequest struct {
	SourceStorageKey string `json:"sourceStorageKey"`
	OutputPrefix     string `json:"outputPrefix"`
	Rows             int    `json:"rows"`
	Columns          int    `json:"columns"`
	PanelCount       int    `json:"panelCount"`
	PanelAspectRatio string `json:"panelAspectRatio"`
}

type StoryboardSheetCropRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type StoryboardSheetPanelResult struct {
	Ordinal     int                     `json:"ordinal"`
	StorageKey  string                  `json:"storageKey"`
	MimeType    string                  `json:"mimeType"`
	ByteSize    int64                   `json:"byteSize"`
	ContentHash string                  `json:"contentHash"`
	Width       int                     `json:"width"`
	Height      int                     `json:"height"`
	Crop        StoryboardSheetCropRect `json:"crop"`
	Put         storage.PutResult       `json:"-"`
}

type StoryboardSheetCropResult struct {
	SourceWidth  int                          `json:"sourceWidth"`
	SourceHeight int                          `json:"sourceHeight"`
	Panels       []StoryboardSheetPanelResult `json:"panels"`
}

func CropStoryboardSheet(ctx context.Context, request StoryboardSheetCropRequest, objectStore ObjectStore) (StoryboardSheetCropResult, error) {
	if objectStore == nil {
		return StoryboardSheetCropResult{}, fmt.Errorf("object storage is required")
	}
	if strings.TrimSpace(request.SourceStorageKey) == "" || strings.TrimSpace(request.OutputPrefix) == "" ||
		request.Rows <= 0 || request.Columns <= 0 || request.PanelCount <= 0 || request.PanelCount > request.Rows*request.Columns {
		return StoryboardSheetCropResult{}, fmt.Errorf("storyboard sheet crop request is invalid")
	}
	body, _, err := objectStore.GetObject(ctx, request.SourceStorageKey, DefaultMaxStoryboardSheetBytes)
	if err != nil {
		return StoryboardSheetCropResult{}, fmt.Errorf("download storyboard sheet: %w", err)
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return StoryboardSheetCropResult{}, fmt.Errorf("decode storyboard sheet: %w", err)
	}
	bounds := source.Bounds()
	if bounds.Dx() < request.Columns*64 || bounds.Dy() < request.Rows*64 {
		return StoryboardSheetCropResult{}, fmt.Errorf("storyboard sheet resolution %dx%d is too small for %dx%d panels", bounds.Dx(), bounds.Dy(), request.Rows, request.Columns)
	}
	targetWidth, targetHeight := parseMediaAspectRatio(request.PanelAspectRatio)
	tempDir, err := os.MkdirTemp("", "cineweave-storyboard-sheet-*")
	if err != nil {
		return StoryboardSheetCropResult{}, err
	}
	defer os.RemoveAll(tempDir)
	result := StoryboardSheetCropResult{
		SourceWidth: bounds.Dx(), SourceHeight: bounds.Dy(),
		Panels: make([]StoryboardSheetPanelResult, 0, request.PanelCount),
	}
	for index := 0; index < request.PanelCount; index++ {
		row, column := index/request.Columns, index%request.Columns
		cell := image.Rect(
			bounds.Min.X+(column*bounds.Dx()/request.Columns),
			bounds.Min.Y+(row*bounds.Dy()/request.Rows),
			bounds.Min.X+((column+1)*bounds.Dx()/request.Columns),
			bounds.Min.Y+((row+1)*bounds.Dy()/request.Rows),
		)
		crop := centerCropRect(cell, targetWidth, targetHeight)
		panel := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(panel, panel.Bounds(), source, crop.Min, draw.Src)
		path := filepath.Join(tempDir, fmt.Sprintf("panel-%02d.png", index+1))
		file, err := os.Create(path)
		if err != nil {
			return StoryboardSheetCropResult{}, err
		}
		encodeErr := png.Encode(file, panel)
		closeErr := file.Close()
		if encodeErr != nil {
			return StoryboardSheetCropResult{}, fmt.Errorf("encode storyboard panel %d: %w", index+1, encodeErr)
		}
		if closeErr != nil {
			return StoryboardSheetCropResult{}, closeErr
		}
		storageKey := strings.TrimRight(strings.TrimSpace(request.OutputPrefix), "/") + fmt.Sprintf("/panel-%02d.png", index+1)
		put, err := objectStore.PutFile(ctx, storageKey, path, "image/png")
		if err != nil {
			return StoryboardSheetCropResult{}, fmt.Errorf("upload storyboard panel %d: %w", index+1, err)
		}
		result.Panels = append(result.Panels, StoryboardSheetPanelResult{
			Ordinal: index + 1, StorageKey: put.StorageKey, MimeType: "image/png",
			ByteSize: put.ByteSize, ContentHash: put.ContentHash,
			Width: crop.Dx(), Height: crop.Dy(),
			Crop: StoryboardSheetCropRect{X: crop.Min.X - bounds.Min.X, Y: crop.Min.Y - bounds.Min.Y, Width: crop.Dx(), Height: crop.Dy()},
			Put:  put,
		})
	}
	return result, nil
}

func centerCropRect(cell image.Rectangle, targetWidth, targetHeight int) image.Rectangle {
	if targetWidth <= 0 || targetHeight <= 0 {
		return cell
	}
	scale := cell.Dx() / targetWidth
	if heightScale := cell.Dy() / targetHeight; heightScale < scale {
		scale = heightScale
	}
	if scale < 1 {
		return cell
	}
	croppedWidth, croppedHeight := targetWidth*scale, targetHeight*scale
	x := cell.Min.X + (cell.Dx()-croppedWidth)/2
	y := cell.Min.Y + (cell.Dy()-croppedHeight)/2
	return image.Rect(x, y, x+croppedWidth, y+croppedHeight)
}

func parseMediaAspectRatio(value string) (int, int) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 16, 9
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 16, 9
	}
	return width, height
}
