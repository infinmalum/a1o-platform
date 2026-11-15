package model

import (
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
)

// Run event retention: RunEvent logs belong to their own retention policy,
// separate from the run rows (runtime plan §4). Pruning targets quiet runs
// only — an active run's updated_time moves on every leg, so its events stay
// available for reconnect replay; a terminal or parked run whose state has
// not changed past the cutoff loses its whole log, which the events stream
// answers with an explicit resync instead of a gapped replay.

// PruneQuietRunEvents deletes the event logs of runs that have not been
// updated since the given unix cutoff. The subquery targets a different
// table, which keeps the statement portable across SQLite, MySQL, and
// PostgreSQL.
func PruneQuietRunEvents(cutoff int64) (int64, error) {
	result := DB.Where("run_id IN (SELECT id FROM agent_runs WHERE updated_time < ?)", cutoff).
		Delete(&RunEvent{})
	return result.RowsAffected, result.Error
}

// RunEventRetentionLoop prunes quiet run event logs on a fixed cadence; a
// non-positive retention days disables it entirely.
func RunEventRetentionLoop() {
	retentionDays := config.RunEventRetentionDays
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Unix() - int64(retentionDays)*24*int64(time.Hour/time.Second)
		pruned, err := PruneQuietRunEvents(cutoff)
		if err != nil {
			logger.SysError("run event retention prune failed: " + err.Error())
			continue
		}
		if pruned > 0 {
			logger.SysLogf("run event retention pruned %d events (cutoff %d)", pruned, cutoff)
		}
	}
}
