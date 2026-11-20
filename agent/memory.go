package agent

import (
	"path/filepath"
	"strconv"

	"github.com/infinmalum/Lapua/memory"

	"github.com/infinmalum/a1o-platform/common/config"
)

// userMemoryStore roots one agent memory directory per user under the
// shared data root, mirroring the image-asset layout. The SDK keeps plain
// markdown files there, so admins can inspect or prune them on disk.
func userMemoryStore(userId int) *memory.Store {
	return memory.NewStore(filepath.Join("data", "memory", "users", strconv.Itoa(userId)))
}

func addMemoryTools(set *toolSet, userId int) {
	if !config.ChatMemoryEnabled {
		return
	}
	for _, core := range memory.Tools(userMemoryStore(userId)) {
		set.addSDK(core)
	}
}
