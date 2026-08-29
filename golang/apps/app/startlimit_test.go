package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
)

// The start watchdog must outlive what the settings PERMIT a start to take.
// Otherwise it kills a healthy start rather than a hung one: on 28 August, with
// limits of 20 and 180, it stood at 90 and brought the process down four times,
// including the rehearsal before the demo.
func TestTheStartOutlivesWhatTheSettingsAllowIt(t *testing.T) {
	cfg := config.Config{
		ThreadResumeLimit: 20 * time.Second,
		AgentCallTimeout:  180 * time.Second,
	}
	limit := startLimitFor(cfg)

	assert.Greater(t, limit, cfg.ThreadResumeLimit+cfg.AgentCallTimeout,
		"a failed resume and starting a new one are TWO requests in a row, and both are permitted")
	assert.Equal(t, 300*time.Second, limit)
}

// A deployment that opens no conversation at all must still have time to reach
// the database, the broker and the envelope.
func TestWithoutAConversationTheFloorStands(t *testing.T) {
	assert.Equal(t, startFloor, startLimitFor(config.Config{}))
}

// Tiny limits must not make the watchdog harsher than the floor: before the
// conversation the process still goes to the database and the broker, and that
// time is not counted in the limits.
func TestSmallLimitsDoNotShrinkTheStartBelowTheFloor(t *testing.T) {
	cfg := config.Config{ThreadResumeLimit: time.Second, AgentCallTimeout: time.Second}
	assert.Equal(t, startFloor, startLimitFor(cfg))
}
