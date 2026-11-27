package model

import (
	"encoding/json"
	"errors"

	"github.com/infinmalum/a1o-platform/common/config"
)

const knowledgeLimitsOption = "KnowledgeLimits"

var defaultKnowledgeLimits = KnowledgeLimits{FileMiB: 16, LibraryMiB: 256}

type KnowledgeLimits struct {
	FileMiB    int `json:"fileMiB"`
	LibraryMiB int `json:"libraryMiB"`
}

func (limits KnowledgeLimits) FileBytes() int    { return limits.FileMiB << 20 }
func (limits KnowledgeLimits) LibraryBytes() int { return limits.LibraryMiB << 20 }

func parseKnowledgeLimits(value string) (KnowledgeLimits, error) {
	var limits KnowledgeLimits
	if err := json.Unmarshal([]byte(value), &limits); err != nil ||
		limits.FileMiB < 1 || limits.FileMiB > 64 ||
		limits.LibraryMiB < limits.FileMiB || limits.LibraryMiB > 1024 {
		return KnowledgeLimits{}, errors.New("knowledge limits require a file size of 1–64 MiB and a library size up to 1024 MiB, at least as large as the file size")
	}
	return limits, nil
}

func GetKnowledgeLimits() KnowledgeLimits {
	config.OptionMapRWMutex.RLock()
	value := config.OptionMap[knowledgeLimitsOption]
	config.OptionMapRWMutex.RUnlock()
	if limits, err := parseKnowledgeLimits(value); err == nil {
		return limits
	}
	return defaultKnowledgeLimits
}

func defaultKnowledgeOptions() string {
	value, _ := json.Marshal(defaultKnowledgeLimits)
	return string(value)
}
