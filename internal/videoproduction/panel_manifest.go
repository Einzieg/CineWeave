package videoproduction

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	PanelManifestContractVersion = "storyboard-sheet-panel-manifest/v1"
	CodePanelManifestInvalid     = "PANEL_MANIFEST_INVALID"
)

type PanelManifestCompileInput struct {
	StoryboardShotID     string
	PlannedDurationTicks int64
	TimelineTimebase     int64
	VideoAspectRatio     string
	EntryState           ShotState
	ExitState            ShotState
}

type PanelManifest struct {
	ContractVersion      string      `json:"contractVersion"`
	StoryboardShotID     string      `json:"storyboardShotId"`
	PlannedDurationTicks int64       `json:"plannedDurationTicks"`
	TimelineTimebase     int64       `json:"timelineTimebase"`
	VideoAspectRatio     string      `json:"videoAspectRatio"`
	SheetAspectRatio     string      `json:"sheetAspectRatio"`
	Rows                 int         `json:"rows"`
	Columns              int         `json:"columns"`
	PanelCount           int         `json:"panelCount"`
	EntryStateHash       string      `json:"entryStateHash"`
	ExitStateHash        string      `json:"exitStateHash"`
	Panels               []PanelSpec `json:"panels"`
	ManifestHash         string      `json:"manifestHash"`
}

type PanelSpec struct {
	Ordinal            int       `json:"ordinal"`
	GridRow            int       `json:"gridRow"`
	GridColumn         int       `json:"gridColumn"`
	TimeTick           int64     `json:"timeTick"`
	NormalizedPosition int       `json:"normalizedPosition"`
	Stage              string    `json:"stage"`
	ActionStage        string    `json:"actionStage"`
	ExpectedState      ShotState `json:"expectedState"`
}

func CompilePanelManifest(input PanelManifestCompileInput) (PanelManifest, error) {
	input.StoryboardShotID = strings.TrimSpace(input.StoryboardShotID)
	if input.StoryboardShotID == "" || input.PlannedDurationTicks <= 0 || input.TimelineTimebase <= 0 {
		return PanelManifest{}, Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 缺少镜头、时长或时间基准"}
	}
	input.EntryState = NormalizeShotState(input.EntryState)
	input.ExitState = NormalizeShotState(input.ExitState)
	if err := ValidateShotState(input.EntryState); err != nil {
		return PanelManifest{}, Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 首帧状态无效：" + err.Error()}
	}
	if err := ValidateShotState(input.ExitState); err != nil {
		return PanelManifest{}, Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 尾帧状态无效：" + err.Error()}
	}
	entryHash, err := HashShotState(input.EntryState)
	if err != nil {
		return PanelManifest{}, err
	}
	exitHash, err := HashShotState(input.ExitState)
	if err != nil {
		return PanelManifest{}, err
	}
	videoAspectRatio := normalizePanelAspectRatio(input.VideoAspectRatio)
	panelCount := panelCountForDuration(input.PlannedDurationTicks, input.TimelineTimebase)
	rows, columns, sheetAspectRatio := panelGrid(panelCount, videoAspectRatio)
	panels := make([]PanelSpec, 0, panelCount)
	for index := 0; index < panelCount; index++ {
		position := 0
		if panelCount > 1 {
			position = int(math.Round(float64(index) * 1000 / float64(panelCount-1)))
		}
		tick := int64(math.Round(float64(input.PlannedDurationTicks) * float64(position) / 1000))
		state := input.EntryState
		stage := panelStage(index, panelCount)
		if index == panelCount-1 {
			state = input.ExitState
		}
		panels = append(panels, PanelSpec{
			Ordinal: index + 1, GridRow: index / columns, GridColumn: index % columns,
			TimeTick: tick, NormalizedPosition: position, Stage: stage,
			ActionStage:   panelActionStage(input.EntryState.Action, input.ExitState.Action, stage),
			ExpectedState: state,
		})
	}
	manifest := PanelManifest{
		ContractVersion: PanelManifestContractVersion, StoryboardShotID: input.StoryboardShotID,
		PlannedDurationTicks: input.PlannedDurationTicks, TimelineTimebase: input.TimelineTimebase,
		VideoAspectRatio: videoAspectRatio, SheetAspectRatio: sheetAspectRatio,
		Rows: rows, Columns: columns, PanelCount: panelCount,
		EntryStateHash: entryHash, ExitStateHash: exitHash, Panels: panels,
	}
	hash, err := HashCanonicalContract(panelManifestHashPayload(manifest))
	if err != nil {
		return PanelManifest{}, err
	}
	manifest.ManifestHash = hash
	if err := ValidatePanelManifest(manifest); err != nil {
		return PanelManifest{}, err
	}
	return manifest, nil
}

func ValidatePanelManifest(manifest PanelManifest) error {
	if manifest.ContractVersion != PanelManifestContractVersion || strings.TrimSpace(manifest.StoryboardShotID) == "" ||
		manifest.PlannedDurationTicks <= 0 || manifest.TimelineTimebase <= 0 || manifest.Rows <= 0 || manifest.Columns <= 0 ||
		manifest.PanelCount < 3 || manifest.PanelCount > 6 || len(manifest.Panels) != manifest.PanelCount ||
		!validSHA256(manifest.EntryStateHash) || !validSHA256(manifest.ExitStateHash) || !validSHA256(manifest.ManifestHash) {
		return Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 基础字段无效"}
	}
	if manifest.Rows*manifest.Columns < manifest.PanelCount {
		return Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 网格无法容纳全部面板"}
	}
	seenCells := map[string]bool{}
	previousTick := int64(-1)
	for index, panel := range manifest.Panels {
		if panel.Ordinal != index+1 || panel.GridRow < 0 || panel.GridRow >= manifest.Rows || panel.GridColumn < 0 || panel.GridColumn >= manifest.Columns ||
			panel.TimeTick < 0 || panel.TimeTick > manifest.PlannedDurationTicks || panel.TimeTick < previousTick || panel.NormalizedPosition < 0 || panel.NormalizedPosition > 1000 ||
			strings.TrimSpace(panel.Stage) == "" || strings.TrimSpace(panel.ActionStage) == "" {
			return Error{Code: CodePanelManifestInvalid, Message: fmt.Sprintf("PanelManifest 第 %d 个面板无效", index+1)}
		}
		cell := strconv.Itoa(panel.GridRow) + ":" + strconv.Itoa(panel.GridColumn)
		if seenCells[cell] {
			return Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 面板网格位置重复"}
		}
		seenCells[cell] = true
		if err := ValidateShotState(panel.ExpectedState); err != nil {
			return Error{Code: CodePanelManifestInvalid, Message: fmt.Sprintf("PanelManifest 第 %d 个面板状态无效：%s", index+1, err)}
		}
		previousTick = panel.TimeTick
	}
	if manifest.Panels[0].TimeTick != 0 || manifest.Panels[0].Stage != "entry" ||
		manifest.Panels[len(manifest.Panels)-1].TimeTick != manifest.PlannedDurationTicks || manifest.Panels[len(manifest.Panels)-1].Stage != "exit" {
		return Error{Code: CodePanelManifestInvalid, Message: "PanelManifest 必须以镜头首状态开始并以尾状态结束"}
	}
	expectedHash, err := HashCanonicalContract(panelManifestHashPayload(manifest))
	if err != nil {
		return err
	}
	if expectedHash != normalizeSHA256(manifest.ManifestHash) {
		return Error{Code: CodePanelManifestInvalid, Message: "PanelManifest hash 不匹配"}
	}
	return nil
}

func panelManifestHashPayload(manifest PanelManifest) any {
	manifest.ManifestHash = ""
	return manifest
}

func panelCountForDuration(durationTicks, timebase int64) int {
	seconds := float64(durationTicks) / float64(timebase)
	switch {
	case seconds <= 5:
		return 3
	case seconds <= 10:
		return 4
	default:
		return 6
	}
}

func panelGrid(panelCount int, videoAspectRatio string) (rows, columns int, sheetAspectRatio string) {
	width, height := parsePanelAspectRatio(videoAspectRatio)
	landscape := width >= height
	switch panelCount {
	case 3:
		if landscape {
			return 3, 1, "2:3"
		}
		return 1, 3, "3:2"
	case 4:
		return 2, 2, videoAspectRatio
	default:
		if landscape {
			return 3, 2, "1:1"
		}
		return 2, 3, "1:1"
	}
}

func normalizePanelAspectRatio(value string) string {
	width, height := parsePanelAspectRatio(value)
	return strconv.Itoa(width) + ":" + strconv.Itoa(height)
}

func parsePanelAspectRatio(value string) (int, int) {
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

func panelStage(index, count int) string {
	if index == 0 {
		return "entry"
	}
	if index == count-1 {
		return "exit"
	}
	position := float64(index) / float64(count-1)
	switch {
	case position < 0.34:
		return "early"
	case position < 0.67:
		return "middle"
	default:
		return "late"
	}
}

func panelActionStage(entry, exit ActionState, stage string) string {
	start := strings.TrimSpace(entry.Entry)
	end := strings.TrimSpace(exit.Exit)
	switch stage {
	case "entry":
		return start
	case "exit":
		return end
	case "early":
		return "从“" + start + "”开始进入动作"
	case "late":
		return "接近“" + end + "”的动作阶段"
	default:
		return "从“" + start + "”向“" + end + "”过渡的中间动作"
	}
}
