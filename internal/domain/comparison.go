package domain

import (
	"strings"
	"time"
)

// 比较任务逐项状态。单项失败不会影响同一任务中的其他项目。
const (
	ComparisonItemPending   = "pending"
	ComparisonItemRunning   = "running"
	ComparisonItemCompleted = "completed"
	ComparisonItemFailed    = "failed"
)

// 比较任务整体状态。只要还有未结束项目即为 running。
const (
	ComparisonJobQueued    = "queued"
	ComparisonJobRunning   = "running"
	ComparisonJobCompleted = "completed"
)

// 逐项失败的稳定原因代码。
const (
	ComparisonReasonReferenceMissing = "reference_missing"
	ComparisonReasonReferenceVersion = "reference_version_invalid"
	ComparisonReasonTargetMissing    = "target_not_found"
	ComparisonReasonTargetVersion    = "target_version_invalid"
)

// ComparisonTarget 标识一个待比较目标档案及其版本。
type ComparisonTarget struct {
	ArtifactID string `json:"artifact_id"`
	// Version 为 0 表示任务创建时该档案的当前版本。
	Version int `json:"version,omitempty"`
}

// ComparisonRequest 是提交比较任务的输入：一条引用加多条目标。
type ComparisonRequest struct {
	Actor     string             `json:"actor,omitempty"`
	Reference ComparisonTarget   `json:"reference"`
	Targets   []ComparisonTarget `json:"targets"`
}

// FieldDiff 描述单个标量字段的取值差异。
type FieldDiff struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// TagSetDiff 描述标签集合的增删。
type TagSetDiff struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

// FieldDifference 是一个字段级差异条目。
type FieldDifference struct {
	Field string      `json:"field"`
	Text  *FieldDiff  `json:"text,omitempty"`
	Tags  *TagSetDiff `json:"tags,omitempty"`
}

// ComparisonItemResult 保存单项比较结论；结论绑定当时的版本快照。
type ComparisonItemResult struct {
	ReferenceID      string            `json:"reference_id"`
	ReferenceVersion int               `json:"reference_version"`
	TargetID         string            `json:"target_id"`
	TargetVersion    int               `json:"target_version"`
	Equal            bool              `json:"equal"`
	Differences      []FieldDifference `json:"differences"`
}

// ComparisonItem 是任务中的一个比较项目，独立记录状态与失败原因。
type ComparisonItem struct {
	ID         string                `json:"id"`
	Reference  ComparisonTarget      `json:"reference"`
	Target     ComparisonTarget      `json:"target"`
	Status     string                `json:"status"`
	Result     *ComparisonItemResult `json:"result,omitempty"`
	ErrorCode  string                `json:"error_code,omitempty"`
	Error      string                `json:"error,omitempty"`
	StartedAt  *time.Time            `json:"started_at,omitempty"`
	FinishedAt *time.Time            `json:"finished_at,omitempty"`
}

// ComparisonJob 是一次提交产生的比较任务。
type ComparisonJob struct {
	ID             string           `json:"id"`
	Fingerprint    string           `json:"fingerprint"`
	Actor          string           `json:"actor,omitempty"`
	Status         string           `json:"status"`
	Reference      ComparisonTarget `json:"reference"`
	Items          []ComparisonItem `json:"items"`
	Reused         bool             `json:"reused,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	FinishedAt     *time.Time       `json:"finished_at,omitempty"`
	CompletedItems int              `json:"completed_items"`
	FailedItems    int              `json:"failed_items"`
	PendingItems   int              `json:"pending_items"`
}

// Validate 校验持久化的比较任务结构是否自洽。
func (j ComparisonJob) Validate() error {
	if strings.TrimSpace(j.ID) == "" {
		return Invalid("id", "comparison job id is required")
	}
	if strings.TrimSpace(j.Fingerprint) == "" {
		return Invalid("fingerprint", "comparison fingerprint is required")
	}
	switch j.Status {
	case ComparisonJobQueued, ComparisonJobRunning, ComparisonJobCompleted:
	default:
		return Invalid("status", "unknown comparison job status")
	}
	if len(j.Items) == 0 {
		return Invalid("items", "comparison job must contain items")
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() {
		return Invalid("timestamps", "comparison job timestamps are required")
	}
	for _, item := range j.Items {
		switch item.Status {
		case ComparisonItemPending, ComparisonItemRunning, ComparisonItemCompleted, ComparisonItemFailed:
		default:
			return Invalid("items", "unknown comparison item status")
		}
	}
	return nil
}

// Snapshot 是档案在某一版本上的不可变内容，用于把比较结论绑定到历史版本。
type ArtifactSnapshot struct {
	Artifact
}
