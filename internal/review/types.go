package review

type Action struct {
	Label      string `json:"label"`
	ActionType string `json:"actionType"`
	Href       string `json:"href,omitempty"`
	TargetType string `json:"targetType,omitempty"`
	TargetID   string `json:"targetId,omitempty"`
}

type ReviewItemDraft struct {
	ItemType          string         `json:"itemType"`
	Category          string         `json:"category"`
	Severity          string         `json:"severity"`
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	Suggestion        string         `json:"suggestion,omitempty"`
	EntityType        string         `json:"entityType"`
	EntityID          string         `json:"entityId,omitempty"`
	RelatedEntityType string         `json:"relatedEntityType,omitempty"`
	RelatedEntityID   string         `json:"relatedEntityId,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type Summary struct {
	OverallStatus string `json:"overallStatus"`
	OpenCount     int    `json:"openCount"`
	CriticalCount int    `json:"criticalCount"`
	HighCount     int    `json:"highCount"`
	MediumCount   int    `json:"mediumCount"`
	LowCount      int    `json:"lowCount"`
}

func Summarize(items []ReviewItemDraft) Summary {
	summary := Summary{OverallStatus: "healthy", OpenCount: len(items)}
	for _, item := range items {
		switch item.Severity {
		case "critical":
			summary.CriticalCount++
		case "high":
			summary.HighCount++
		case "low":
			summary.LowCount++
		default:
			summary.MediumCount++
		}
	}
	if summary.CriticalCount > 0 {
		summary.OverallStatus = "blocked"
	} else if summary.HighCount > 0 || summary.MediumCount > 0 || summary.LowCount > 0 {
		summary.OverallStatus = "needs_attention"
	}
	return summary
}
