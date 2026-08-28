package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
)

// Сторож старта обязан переживать то, что настройки старту РАЗРЕШАЮТ. Иначе он
// убивает не зависший запуск, а исправный: 28 августа при пределах 20 и 180 он
// стоял на 90 и уронил процесс четырежды, включая репетицию перед стендом.
func TestTheStartOutlivesWhatTheSettingsAllowIt(t *testing.T) {
	cfg := config.Config{
		ThreadResumeLimit: 20 * time.Second,
		AgentCallTimeout:  180 * time.Second,
	}
	limit := startLimitFor(cfg)

	assert.Greater(t, limit, cfg.ThreadResumeLimit+cfg.AgentCallTimeout,
		"неудавшееся возобновление и запуск нового - это ДВА запроса подряд, и оба разрешены")
	assert.Equal(t, 300*time.Second, limit)
}

// Развёртывание, которое разговора не открывает вовсе, всё равно должно успеть
// дойти до базы, брокера и конверта.
func TestWithoutAConversationTheFloorStands(t *testing.T) {
	assert.Equal(t, startFloor, startLimitFor(config.Config{}))
}

// Крошечные пределы не должны делать сторожа злее пола: до разговора процесс
// успевает сходить в базу и к брокеру, и это время в пределах не учтено.
func TestSmallLimitsDoNotShrinkTheStartBelowTheFloor(t *testing.T) {
	cfg := config.Config{ThreadResumeLimit: time.Second, AgentCallTimeout: time.Second}
	assert.Equal(t, startFloor, startLimitFor(cfg))
}
