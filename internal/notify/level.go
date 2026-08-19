package notify

import (
	"os"
	"strings"

	"smallctl/internal/general"
)

// Level defines when notifications are sent.
type Level string

var (
	LevelOff   = Level(general.NotifyLevelOff)
	LevelError = Level(general.NotifyLevelError)
	LevelAll   = Level(general.NotifyLevelAll)
)

func ParseLevel(s string) Level {
	switch s {
	case general.NotifyLevelError:
		return LevelError
	case general.NotifyLevelAll:
		return LevelAll
	default:
		return LevelOff
	}
}

// ResolveLevel determines the effective notification level: the
// $SMALLCTL_NOTIFY environment variable overrides the config value.
func ResolveLevel(configValue string) Level {
	levelStr := strings.TrimSpace(os.Getenv("SMALLCTL_NOTIFY"))
	if levelStr == "" {
		levelStr = configValue
	}
	return ParseLevel(strings.ToLower(levelStr))
}
