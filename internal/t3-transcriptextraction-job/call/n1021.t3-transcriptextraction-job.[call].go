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
	firstByteAt       time.Time
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
		nowTime := time.Now()
		if watchBuffer.firstByteAt.IsZero() {
			watchBuffer.firstByteAt = nowTime
		}
		watchBuffer.lastGrowthAt = nowTime
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

// SecondsSinceFirstByte returns how long the call has been writing, or 0 when
// it has written nothing yet.
//
// The provider this pipeline uses does not stream: it holds the whole answer
// and writes it at the end. A measured 181224-char prompt answered in 330s
// with its first byte at 328s. So the time before the first byte is the
// provider thinking, not the call stalling, and no rule may judge the call
// until that byte arrives.
func (watchBuffer *StallWatchBuffer) SecondsSinceFirstByte() float64 {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	if watchBuffer.firstByteAt.IsZero() {
		return 0
	}
	return time.Since(watchBuffer.firstByteAt).Seconds()
}

// HasFirstByte reports whether the call has written anything.
func (watchBuffer *StallWatchBuffer) HasFirstByte() bool {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	return !watchBuffer.firstByteAt.IsZero()
}

// CharRatePerSecond returns the output rate since the first byte.
//
// The rate is measured from the first byte, not from the start of the call,
// because the provider holds the whole answer and writes it at the end. A
// call that has written nothing has no rate yet, and the no-growth rule
// bounds it instead.
//
// The rate is measured from the first byte rather than from the last growth
// because a call that writes 1 character every 2 seconds keeps its growth
// clock moving, so the no-growth rule never fires on it, and only this rate
// shows that it is too slow to finish.
func (watchBuffer *StallWatchBuffer) CharRatePerSecond() float64 {
	watchBuffer.lockHandle.Lock()
	defer watchBuffer.lockHandle.Unlock()
	if watchBuffer.firstByteAt.IsZero() {
		return 0
	}
	elapsedSeconds := time.Since(watchBuffer.firstByteAt).Seconds()
	if elapsedSeconds <= 0 {
		return 0
	}
	return float64(watchBuffer.outputBuffer.Len()) / elapsedSeconds
}