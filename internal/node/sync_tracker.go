package node

import (
	"sync"
	"time"

	"aichain/internal/protocol"
)

type SyncTracker struct {
	mu     sync.RWMutex
	status protocol.SyncStatus
}

func NewSyncTracker() *SyncTracker {
	return &SyncTracker{}
}

func (t *SyncTracker) RecordAttempt(mode string, peer string, forkHeight int64, targetHeight int64) {
	now := time.Now().UTC()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.LastAttemptAt = &now
	t.status.LastMode = mode
	t.status.LastPeer = peer
	t.status.LastForkHeight = forkHeight
	t.status.LastTargetHeight = targetHeight
}

func (t *SyncTracker) RecordSuccess(mode string, peer string, forkHeight int64, targetHeight int64, importedHeight int64) {
	now := time.Now().UTC()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.LastAttemptAt = &now
	t.status.LastSuccessAt = &now
	t.status.LastMode = mode
	t.status.LastPeer = peer
	t.status.LastForkHeight = forkHeight
	t.status.LastTargetHeight = targetHeight
	t.status.LastImportedHeight = importedHeight
	t.status.LastError = ""
}

func (t *SyncTracker) RecordFailure(mode string, peer string, forkHeight int64, targetHeight int64, err error) {
	now := time.Now().UTC()

	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.LastAttemptAt = &now
	t.status.LastFailureAt = &now
	t.status.LastMode = mode
	t.status.LastPeer = peer
	t.status.LastForkHeight = forkHeight
	t.status.LastTargetHeight = targetHeight
	if err != nil {
		t.status.LastError = err.Error()
	}
}

func (t *SyncTracker) SyncStatus() protocol.SyncStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := t.status
	status.LastAttemptAt = cloneTime(status.LastAttemptAt)
	status.LastSuccessAt = cloneTime(status.LastSuccessAt)
	status.LastFailureAt = cloneTime(status.LastFailureAt)
	return status
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

