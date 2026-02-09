package configmanager

import (
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
)

func (m *ConfigManager) notifyLoadingStart() {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Load config...",
		Emoji:   "⏳",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyLoadingConfig() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "loading ksail config",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyUsingDefaults() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "using default config",
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyConfigFound() {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "'%s' found",
		Args:    []any{m.Viper.ConfigFileUsed()},
		Writer:  m.Writer,
	})
}

func (m *ConfigManager) notifyLoadingComplete(tmr timer.Timer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "config loaded",
		Timer:   tmr,
		Writer:  m.Writer,
	})
}
