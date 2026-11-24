package call

import (
	"bytes"
	"sync"
	"time"
)

// StallWatchBuffer collects a command's output and records when it last grew.
//
// A call that hangs holds its worker for the whole timeout, so the runner
// needs to know whether the output is still moving. The buffer is written by
// the child's output goroutine and read by the stall watcher, so every field
// is guarded by the lock.
type StallWatchBuffer struct {
	lockHandle        sync.Mutex
	outputBuffer      bytes.Buffer
	lastGrowthAt      time.Time
	growthCount       int
	lastGrowthCharLen int
}

// NewStallWatchBuffer returns an empty buffer whose growth clock starts now.
func NewStallWatchBuffer() *StallWatchBuffer {
	return &StallWatchBuffer{lastGrowthAt: time.Now()}
}

// Write appends the output and moves the growth clock.
func (watchBuffer *StallWatchBuffer) Write(outputBytes []byte) (int, error) {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	writtenCount, writeError := watchBuffer.outputBuffer.Write(outputBytes)
	if writtenCount > 0 {
		watchBuffer.lastGrowthAt = time.Now()
		watchBuffer.growthCount++
		watchBuffer.lastGrowthCharLen = watchBuffer.outputBuffer.Len()
	}
	return writtenCount, writeError
}

// Text returns the output collected so far.
func (watchBuffer *StallWatchBuffer) Text() string {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return watchBuffer.outputBuffer.String()
}

// CharLen returns the number of characters collected so far.
func (watchBuffer *StallWatchBuffer) CharLen() int {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return watchBuffer.outputBuffer.Len()
}

// SecondsSinceGrowth returns how long the output has stood still.
func (watchBuffer *StallWatchBuffer) SecondsSinceGrowth() float64 {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return time.Since(watchBuffer.lastGrowthAt).Seconds()
}

// GrowthCount returns how many writes have arrived.
func (watchBuffer *StallWatchBuffer) GrowthCount() int {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return watchBuffer.growthCount
}