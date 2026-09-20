package comparison

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"example.com/solo-0009-archive-weave/internal/domain"
)

// MaximumTargets 限制一次提交的目标数量，防止超大请求拖垮服务。
const MaximumTargets = 256

// NormalizeRequest 校验并规范化比较请求。
// 引用必须在提交时即可解析；目标逐条校验，非法目标不影响其他目标的创建。
func NormalizeRequest(request domain.ComparisonRequest) (domain.ComparisonRequest, error) {
	reference, err := normalizeTarget(request.Reference, "reference")
	if err != nil {
		return domain.ComparisonRequest{}, err
	}
	if len(request.Targets) == 0 {
		return domain.ComparisonRequest{}, domain.Invalid("targets", "at least one comparison target is required")
	}
	if len(request.Targets) > MaximumTargets {
		return domain.ComparisonRequest{}, domain.Invalid("targets",
			fmt.Sprintf("no more than %d comparison targets are allowed", MaximumTargets))
	}
	normalizedTargets := make([]domain.ComparisonTarget, 0, len(request.Targets))
	for index, target := range request.Targets {
		normalized, err := normalizeTarget(target, "targets["+strconv.Itoa(index)+"]")
		if err != nil {
			return domain.ComparisonRequest{}, err
		}
		normalizedTargets = append(normalizedTargets, normalized)
	}
	return domain.ComparisonRequest{
		Actor:     strings.TrimSpace(request.Actor),
		Reference: reference,
		Targets:   normalizedTargets,
	}, nil
}

func normalizeTarget(target domain.ComparisonTarget, field string) (domain.ComparisonTarget, error) {
	id := strings.TrimSpace(target.ArtifactID)
	if id == "" {
		return domain.ComparisonTarget{}, domain.Invalid(field, "artifact id is required")
	}
	if len(id) > 128 {
		return domain.ComparisonTarget{}, domain.Invalid(field, "artifact id is too long")
	}
	if target.Version < 0 {
		return domain.ComparisonTarget{}, domain.Invalid(field, "version must be positive or omitted")
	}
	return domain.ComparisonTarget{ArtifactID: id, Version: target.Version}, nil
}

// Fingerprint 对规范化后的输入计算稳定指纹。相同引用与目标序列（含指定版本）
// 必然得到相同指纹，与操作者和提交时间无关，从而支持结果复用。
func Fingerprint(request domain.ComparisonRequest) string {
	targets := make([][2]string, 0, len(request.Targets))
	for _, target := range request.Targets {
		targets = append(targets, [2]string{target.ArtifactID, strconv.Itoa(target.Version)})
	}
	payload := struct {
		Reference [2]string   `json:"reference"`
		Targets   [][2]string `json:"targets"`
	}{
		Reference: [2]string{request.Reference.ArtifactID, strconv.Itoa(request.Reference.Version)},
		Targets:   targets,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// ListLimitFromQuery 从查询参数解析列表上限，非法值回落到默认上限。
func ListLimitFromQuery(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return listLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return listLimit
	}
	if limit > listLimit {
		return listLimit
	}
	return limit
}
