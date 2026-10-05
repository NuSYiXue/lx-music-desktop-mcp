package musicstore

import "sort"

// 音质等级，与 LX 的 LX.Quality 取值一致。
const (
	Quality128K      = "128k"
	Quality320K      = "320k"
	QualityFLAC      = "flac"
	QualityFLAC24Bit = "flac24bit"
)

// qualityRank 是音质权重：flac24bit > flac > 320k > 128k > 未知(0)。
var qualityRank = map[string]int{
	QualityFLAC24Bit: 4,
	QualityFLAC:      3,
	Quality320K:      2,
	Quality128K:      1,
}

// QualityRank 返回音质等级权重，未知等级为 0。
func QualityRank(q string) int { return qualityRank[q] }

// BestQuality 返回一组音质里最好的一档，全为空或全未知时返回 ""。
func BestQuality(qs []string) string {
	best, bestRank := "", 0
	for _, q := range qs {
		if r := qualityRank[q]; r > bestRank {
			best, bestRank = q, r
		}
	}
	return best
}

// MeetsQuality 判断音质集合是否达到 min 要求。min 为空视为不设限。
func MeetsQuality(qs []string, min string) bool {
	if min == "" {
		return true
	}
	return QualityRank(BestQuality(qs)) >= QualityRank(min)
}

// SortQuality 按从高到低就地排序音质列表。
func SortQuality(qs []string) {
	sort.SliceStable(qs, func(i, j int) bool {
		return qualityRank[qs[i]] > qualityRank[qs[j]]
	})
}
