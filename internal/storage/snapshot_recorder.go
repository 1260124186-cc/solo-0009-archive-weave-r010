package storage

import (
	"context"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// snapshotRepositoryRecorder 在每次成功保存档案前冻结该版本快照。
// 装饰底层 Repository，使既有 catalog 用例无需感知快照机制。
//
// 顺序选择"先快照、后档案"：版本号单调递增，快照先落盘不会覆盖任何历史版本；
// 若随后档案写入失败，再尝试删除刚写入的快照，保持两侧一致。
type snapshotRepositoryRecorder struct {
	inner     Repository
	snapshots SnapshotRepository
}

// NewSnapshottingRepository 包装档案仓储，保存时同步记录版本快照。
func NewSnapshottingRepository(inner Repository, snapshots SnapshotRepository) Repository {
	if snapshots == nil {
		return inner
	}
	return &snapshotRepositoryRecorder{inner: inner, snapshots: snapshots}
}

func (r *snapshotRepositoryRecorder) List(ctx context.Context) ([]domain.Artifact, error) {
	return r.inner.List(ctx)
}

func (r *snapshotRepositoryRecorder) Get(ctx context.Context, id string) (domain.Artifact, error) {
	return r.inner.Get(ctx, id)
}

func (r *snapshotRepositoryRecorder) Save(ctx context.Context, artifact domain.Artifact) error {
	alreadyFrozen, err := r.snapshots.HasVersion(ctx, artifact.ID, artifact.Version)
	if err != nil {
		return err
	}
	if !alreadyFrozen {
		if err := r.snapshots.PutVersion(ctx, artifact); err != nil {
			return err
		}
	}
	if err := r.inner.Save(ctx, artifact); err != nil {
		if !alreadyFrozen {
			// 尽力删除刚冻结、却未被档案存储接受的版本；失败仅意味着留下一个
			// 不会被任何已存在档案引用的孤儿快照，比较解析仍以档案存在性为准。
			if remover, ok := r.snapshots.(interface {
				RemoveVersion(context.Context, string, int) error
			}); ok {
				_ = remover.RemoveVersion(ctx, artifact.ID, artifact.Version)
			}
		}
		return err
	}
	return nil
}

func (r *snapshotRepositoryRecorder) Delete(ctx context.Context, id string) error {
	return r.inner.Delete(ctx, id)
}
