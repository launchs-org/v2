package launchs_stats

import "sort"

// counted は、キーと、その数(上位の集計の 1 行)。
type counted struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// topCounter は、Space-Saving 法で、よく出るキーを、決まった数の枠だけで数える。
// 枠が一杯のとき、新しいキーは、最も小さい枠を奪う(数は、奪った枠の数 + 1)。メモリは、枠の数で上限が決まる。
type topCounter struct {
	// capacity は、枠の数。
	capacity int
	// counts は、キーごとの数。
	counts map[string]int64
}

// newTopCounter は、枠の数を指定して、数えるものを作る。
func newTopCounter(capacity int) *topCounter {
	// 空の表を持つ。
	return &topCounter{capacity: capacity, counts: make(map[string]int64, capacity)}
}

// add は、キーを 1 回数える。
func (counter *topCounter) add(key string) {
	// 既にある枠なら、足すだけ。
	if _, found := counter.counts[key]; found {
		counter.counts[key]++
		return
	}
	// 空きがあれば、新しい枠を使う。
	if len(counter.counts) < counter.capacity {
		counter.counts[key] = 1
		return
	}
	// 一杯なら、最も小さい枠を探して、奪う。
	minKey := ""
	minCount := int64(-1)
	for existing, count := range counter.counts {
		if minCount < 0 || count < minCount || (count == minCount && existing < minKey) {
			minKey = existing
			minCount = count
		}
	}
	delete(counter.counts, minKey)
	counter.counts[key] = minCount + 1
}

// top は、数の多い順(同じ数ならキーの昇順)に、上位 limit 個を返す。
func (counter *topCounter) top(limit int) []counted {
	// 全ての枠を並べる。
	result := make([]counted, 0, len(counter.counts))
	for key, count := range counter.counts {
		result = append(result, counted{Key: key, Count: count})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Count != result[right].Count {
			return result[left].Count > result[right].Count
		}
		return result[left].Key < result[right].Key
	})
	// 上位だけに切る。
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
