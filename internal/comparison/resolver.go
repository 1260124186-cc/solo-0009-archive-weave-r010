package comparison

import (
	"context"
	"errors"
	"fmt"

	"example.com/solo-0009-archive-weave/internal/domain"
	"example.com/solo-0009-archive-weave/internal/storage"
)

// VersionResolver 把 (档案ID, 版本号) 解析为不可变快照。
// version 为 0 表示解析时该档案的当前版本；解析后即固化具体版本号。
type VersionResolver struct {
	snapshots storage.SnapshotRepository
}

func NewVersionResolver(snapshots storage.SnapshotRepository) *VersionResolver {
	return &VersionResolver{snapshots: snapshots}
}

// Resolved 持有解析结果。Artifact 为冻结快照，Version 为固化后的版本号。
type Resolved struct {
	Artifact domain.Artifact
	Version  int
}

// Resolve 解析一个比较目标。
//   - 档案不存在：返回带 not_found 代码的错误；
//   - 档案存在但指定版本不合法：返回带 invalid_version 代码的错误。
func (r *VersionResolver) Resolve(ctx context.Context, target domain.ComparisonTarget) (Resolved, error) {
	if target.Version == 0 {
		artifact, err := r.snapshots.LatestVersion(ctx, target.ArtifactID)
		if err != nil {
			return Resolved{}, classifyMissing(err, target)
		}
		return Resolved{Artifact: artifact, Version: artifact.Version}, nil
	}
	artifact, err := r.snapshots.GetVersion(ctx, target.ArtifactID, target.Version)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Resolved{}, err
		}
		// 指定版本缺失：可能是档案不存在，也可能是版本号非法，用 latest 区分。
		if _, latestErr := r.snapshots.LatestVersion(ctx, target.ArtifactID); latestErr != nil {
			return Resolved{}, classifyMissing(latestErr, target)
		}
		return Resolved{}, InvalidVersionError{
			ArtifactID: target.ArtifactID,
			Version:    target.Version,
		}
	}
	return Resolved{Artifact: artifact, Version: artifact.Version}, nil
}

func classifyMissing(err error, target domain.ComparisonTarget) error {
	var appError domain.AppError
	if errors.As(err, &appError) && appError.Code == domain.ErrNotFound {
		return MissingArtifactError{ArtifactID: target.ArtifactID, Version: target.Version}
	}
	return fmt.Errorf("resolve artifact %s: %w", target.ArtifactID, err)
}

// MissingArtifactError 表示引用或目标档案不存在。
type MissingArtifactError struct {
	ArtifactID string
	Version    int
}

func (e MissingArtifactError) Error() string {
	return fmt.Sprintf("artifact %s does not exist", e.ArtifactID)
}

// InvalidVersionError 表示档案存在但请求的版本号不合法。
type InvalidVersionError struct {
	ArtifactID string
	Version    int
}

func (e InvalidVersionError) Error() string {
	return fmt.Sprintf("artifact %s has no version %d", e.ArtifactID, e.Version)
}
