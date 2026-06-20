package main

import (
	"fmt"
	"math"
)

// Watermark represents a watermark in the stream
type Watermark int64

const (
	MinWatermark Watermark = math.MinInt64
	MaxWatermark Watermark = math.MaxInt64
)

// ChannelStatus represents the status of an input channel
type ChannelStatus int

const (
	Active ChannelStatus = iota
	Idle
)

// InputChannelState represents the state of an input channel in the valve
type InputChannelState struct {
	ChannelIndex  int
	LastWatermark Watermark
	Status        ChannelStatus
}

// StatusWatermarkValve aligns watermarks from multiple input channels
type StatusWatermarkValve struct {
	numChannels      int
	channelStates    []InputChannelState
	lastOutWatermark Watermark
}

// NewStatusWatermarkValve creates a new valve
func NewStatusWatermarkValve(numChannels int) *StatusWatermarkValve {
	states := make([]InputChannelState, numChannels)
	for i := 0; i < numChannels; i++ {
		states[i] = InputChannelState{
			ChannelIndex:  i,
			LastWatermark: MinWatermark,
			Status:        Active,
		}
	}
	return &StatusWatermarkValve{
		numChannels:      numChannels,
		channelStates:    states,
		lastOutWatermark: MinWatermark,
	}
}

// InitializeRecovery initializes the valve states upon recovery to prevent uninitialized channels
// from dragging down the aligned watermark.
func (v *StatusWatermarkValve) InitializeRecovery(restoredWatermark Watermark) {
	v.lastOutWatermark = restoredWatermark
	for i := 0; i < v.numChannels; i++ {
		// Initialize channel watermarks to the restored watermark to prevent dragging it down
		if v.channelStates[i].LastWatermark < restoredWatermark {
			v.channelStates[i].LastWatermark = restoredWatermark
		}
	}
}

// InputWatermark receives a watermark from a channel and returns the new aligned watermark if it advanced
func (v *StatusWatermarkValve) InputWatermark(channelIndex int, watermark Watermark) (Watermark, bool) {
	if channelIndex < 0 || channelIndex >= v.numChannels {
		return v.lastOutWatermark, false
	}

	// Monotonicity check per channel
	if watermark > v.channelStates[channelIndex].LastWatermark {
		v.channelStates[channelIndex].LastWatermark = watermark
	}

	// Find the minimum watermark among all active channels
	minActiveWatermark := MaxWatermark
	hasActiveChannels := false

	for _, state := range v.channelStates {
		if state.Status == Active {
			hasActiveChannels = true
			if state.LastWatermark < minActiveWatermark {
				minActiveWatermark = state.LastWatermark
			}
		}
	}

	var nextWatermark Watermark
	if !hasActiveChannels {
		nextWatermark = v.lastOutWatermark
	} else {
		nextWatermark = minActiveWatermark
	}

	// Monotonicity guarantee for the output watermark
	if nextWatermark > v.lastOutWatermark {
		v.lastOutWatermark = nextWatermark
		return v.lastOutWatermark, true
	}

	return v.lastOutWatermark, false
}

// SetChannelStatus sets the status of a channel (Active/Idle)
func (v *StatusWatermarkValve) SetChannelStatus(channelIndex int, status ChannelStatus) (Watermark, bool) {
	if channelIndex < 0 || channelIndex >= v.numChannels {
		return v.lastOutWatermark, false
	}
	v.channelStates[channelIndex].Status = status
	
	// Re-evaluate aligned watermark
	return v.InputWatermark(channelIndex, v.channelStates[channelIndex].LastWatermark)
}

// Task represents a stateful task/operator
type Task struct {
	ID                  string
	LastEmittedWatermark Watermark
}

// NewTask creates a new task
func NewTask(id string) *Task {
	return &Task{
		ID:                  id,
		LastEmittedWatermark: MinWatermark,
	}
}

// EmitWatermark emits a watermark ensuring monotonicity
func (t *Task) EmitWatermark(watermark Watermark) (Watermark, bool) {
	if watermark > t.LastEmittedWatermark {
		t.LastEmittedWatermark = watermark
		return t.LastEmittedWatermark, true
	}
	return t.LastEmittedWatermark, false
}

// Checkpoint saves the task state
func (t *Task) Checkpoint() Watermark {
	return t.LastEmittedWatermark
}

// Restore restores the task state and guarantees monotonicity
func (t *Task) Restore