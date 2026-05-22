package aiclassifier

import (
	"encoding/json"
	"log"
	"math"
	"sort"
)

// DecisionNode 决策树节点
type DecisionNode struct {
	IsLeaf        bool                    `json:"is_leaf"`
	Label         string                  `json:"label,omitempty"`
	Confidence    float64                 `json:"confidence,omitempty"`
	SampleCount   int                     `json:"sample_count,omitempty"`
	FeatureIndex  int                     `json:"feature_index,omitempty"`
	FeatureName   string                  `json:"feature_name,omitempty"`
	Threshold     float64                 `json:"threshold,omitempty"`
	Left          *DecisionNode           `json:"left,omitempty"`  // <= threshold
	Right         *DecisionNode           `json:"right,omitempty"` // > threshold
	Distribution  map[string]int          `json:"distribution,omitempty"`
}

// LocalClassifier 本地决策树分类器
type LocalClassifier struct {
	Root    *DecisionNode `json:"root"`
	Version int           `json:"version"`
	Ready   bool          `json:"ready"`
}

// NewLocalClassifier 创建空的本地分类器
func NewLocalClassifier() *LocalClassifier {
	return &LocalClassifier{Ready: false}
}

// Predict 使用决策树预测
func (c *LocalClassifier) Predict(features []float64) ClassifyResult {
	if !c.Ready || c.Root == nil {
		return ClassifyResult{Label: "", Confidence: 0, Source: "local_model"}
	}
	node := c.Root
	for node != nil && !node.IsLeaf {
		if node.FeatureIndex >= len(features) {
			break
		}
		if features[node.FeatureIndex] <= node.Threshold {
			node = node.Left
		} else {
			node = node.Right
		}
	}
	if node == nil || !node.IsLeaf {
		return ClassifyResult{Label: TypeBody, Confidence: 0.5, Source: "local_model"}
	}
	return ClassifyResult{
		Label:      node.Label,
		Confidence: node.Confidence,
		Source:     "local_model",
		Level:      detectLevelFromLabel(node.Label),
	}
}

// IsReady 是否已训练完成
func (c *LocalClassifier) IsReady() bool {
	return c.Ready && c.Root != nil
}

// trainingSample 训练样本
type trainingSample struct {
	Features []float64
	Label    string
	Weight   float64
}

// Train 使用 CART 算法训练决策树
func (c *LocalClassifier) Train(samples []trainingSample, maxDepth int) {
	if len(samples) < 10 {
		log.Printf("[决策树] 样本不足 (%d)，跳过训练", len(samples))
		return
	}

	log.Printf("[决策树] 开始训练，样本数=%d, 最大深度=%d", len(samples), maxDepth)
	c.Root = buildTree(samples, 0, maxDepth, 5)
	c.Version++
	c.Ready = true

	// 计算训练准确率
	correct := 0
	for _, s := range samples {
		result := c.Predict(s.Features)
		if result.Label == s.Label {
			correct++
		}
	}
	accuracy := float64(correct) / float64(len(samples))
	log.Printf("[决策树] 训练完成，版本=%d, 训练准确率=%.2f%%", c.Version, accuracy*100)
}

// buildTree 递归构建 CART 决策树
func buildTree(samples []trainingSample, depth, maxDepth, minSamples int) *DecisionNode {
	if len(samples) < minSamples || depth >= maxDepth {
		return makeLeaf(samples)
	}

	// 检查是否所有样本同类
	allSame := true
	firstLabel := samples[0].Label
	for _, s := range samples[1:] {
		if s.Label != firstLabel {
			allSame = false
			break
		}
	}
	if allSame {
		return &DecisionNode{
			IsLeaf:      true,
			Label:       firstLabel,
			Confidence:  1.0,
			SampleCount: len(samples),
			Distribution: map[string]int{firstLabel: len(samples)},
		}
	}

	// 找最佳分裂
	bestFeature, bestThreshold, bestGini := -1, 0.0, math.MaxFloat64
	nFeatures := len(samples[0].Features)

	for fi := 0; fi < nFeatures; fi++ {
		thresholds := uniqueSorted(samples, fi)
		for _, thresh := range thresholds {
			leftCount, rightCount := 0, 0
			leftDist := make(map[string]float64)
			rightDist := make(map[string]float64)
			var leftWeight, rightWeight float64

			for _, s := range samples {
				w := s.Weight
				if w <= 0 {
					w = 1.0
				}
				if s.Features[fi] <= thresh {
					leftDist[s.Label] += w
					leftWeight += w
					leftCount++
				} else {
					rightDist[s.Label] += w
					rightWeight += w
					rightCount++
				}
			}
			if leftCount < 2 || rightCount < 2 {
				continue
			}

			gini := (leftWeight*giniImpurity(leftDist, leftWeight) +
				rightWeight*giniImpurity(rightDist, rightWeight)) /
				(leftWeight + rightWeight)

			if gini < bestGini {
				bestGini = gini
				bestFeature = fi
				bestThreshold = thresh
			}
		}
	}

	if bestFeature < 0 {
		return makeLeaf(samples)
	}

	// 分裂
	var leftSamples, rightSamples []trainingSample
	for _, s := range samples {
		if s.Features[bestFeature] <= bestThreshold {
			leftSamples = append(leftSamples, s)
		} else {
			rightSamples = append(rightSamples, s)
		}
	}

	featureName := ""
	if bestFeature < len(FeatureNames) {
		featureName = FeatureNames[bestFeature]
	}

	return &DecisionNode{
		IsLeaf:       false,
		FeatureIndex: bestFeature,
		FeatureName:  featureName,
		Threshold:    bestThreshold,
		SampleCount:  len(samples),
		Left:         buildTree(leftSamples, depth+1, maxDepth, minSamples),
		Right:        buildTree(rightSamples, depth+1, maxDepth, minSamples),
	}
}

// makeLeaf 创建叶子节点
func makeLeaf(samples []trainingSample) *DecisionNode {
	dist := make(map[string]int)
	weightDist := make(map[string]float64)
	for _, s := range samples {
		dist[s.Label]++
		w := s.Weight
		if w <= 0 {
			w = 1.0
		}
		weightDist[s.Label] += w
	}

	bestLabel := TypeBody
	bestWeight := 0.0
	totalWeight := 0.0
	for label, w := range weightDist {
		totalWeight += w
		if w > bestWeight {
			bestWeight = w
			bestLabel = label
		}
	}
	conf := 0.5
	if totalWeight > 0 {
		conf = bestWeight / totalWeight
	}

	return &DecisionNode{
		IsLeaf:       true,
		Label:        bestLabel,
		Confidence:   conf,
		SampleCount:  len(samples),
		Distribution: dist,
	}
}

// giniImpurity 计算 Gini 不纯度
func giniImpurity(dist map[string]float64, total float64) float64 {
	if total == 0 {
		return 0
	}
	sum := 0.0
	for _, count := range dist {
		p := count / total
		sum += p * p
	}
	return 1 - sum
}

// uniqueSorted 获取某特征列的唯一排序值（用于分裂候选）
func uniqueSorted(samples []trainingSample, featureIdx int) []float64 {
	vals := make(map[float64]bool)
	for _, s := range samples {
		vals[s.Features[featureIdx]] = true
	}
	sorted := make([]float64, 0, len(vals))
	for v := range vals {
		sorted = append(sorted, v)
	}
	sort.Float64s(sorted)

	// 取中点作为候选阈值
	if len(sorted) <= 1 {
		return sorted
	}
	thresholds := make([]float64, 0, len(sorted)-1)
	for i := 0; i < len(sorted)-1; i++ {
		thresholds = append(thresholds, (sorted[i]+sorted[i+1])/2)
	}
	// 限制候选数量
	if len(thresholds) > 20 {
		step := len(thresholds) / 20
		sampled := make([]float64, 0, 20)
		for i := 0; i < len(thresholds); i += step {
			sampled = append(sampled, thresholds[i])
		}
		return sampled
	}
	return thresholds
}

// Serialize 序列化为 JSON
func (c *LocalClassifier) Serialize() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Deserialize 从 JSON 反序列化
func (c *LocalClassifier) Deserialize(data string) error {
	return json.Unmarshal([]byte(data), c)
}
