package ratio

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/infinmalum/a1o-platform/common/logger"
)

var groupRatioLock sync.RWMutex
var GroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

func GroupRatio2JSONString() string {
	groupRatioLock.RLock()
	defer groupRatioLock.RUnlock()
	jsonBytes, err := json.Marshal(GroupRatio)
	if err != nil {
		logger.SysError("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

// GroupRatioNames returns the configured group names, sorted. Callers must
// use this instead of ranging over GroupRatio, which races with option
// updates that swap the map.
func GroupRatioNames() []string {
	groupRatioLock.RLock()
	defer groupRatioLock.RUnlock()
	names := make([]string, 0, len(GroupRatio))
	for name := range GroupRatio {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	groupRatioLock.Lock()
	defer groupRatioLock.Unlock()
	GroupRatio = make(map[string]float64)
	return json.Unmarshal([]byte(jsonStr), &GroupRatio)
}

func GetGroupRatio(name string) float64 {
	groupRatioLock.RLock()
	defer groupRatioLock.RUnlock()
	ratio, ok := GroupRatio[name]
	if !ok {
		logger.SysError("group ratio not found: " + name)
		return 1
	}
	return ratio
}
