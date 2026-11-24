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
	startedAt         time.Time
	lastGrowthAt      time.Time
	growthCount       int
	lastGrowthCharLen int
}

// NewStallWatchBuffer returns an empty buffer whose growth clock starts now.
func NewStallWatchBuffer() *StallWatchBuffer {
	nowTime := time.Now()
	return &StallWatchBuffer{startedAt: nowTime, lastGrowthAt: nowTime}
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

// SecondsSinceStart returns how long the call has been running.
func (watchBuffer *StallWatchBuffer) SecondsSinceStart() float64 {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return time.Since(watchBuffer.startedAt).Seconds()
}

// CharRatePerSecond returns the output rate over the whole call.
//
// The rate is measured from the start, not from the last growth, because the
// trickle rule asks whether the call has produced enough by now. A call that
// writes 1 character every 2 seconds keeps its growth clock moving, so the
// no-growth rule never fires on it, and only this rate shows that it is too
// slow to finish.
func (watchBuffer *StallWatchBuffer) CharRatePerSecond() float64 {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	elapsedSeconds := time.Since(watchBuffer.startedAt).Seconds()
	if elapsedSeconds <= 0 {
		return 0
	}
	return float64(watchBuffer.outputBuffer.Len()) / elapsedSeconds
}