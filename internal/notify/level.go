package notify

import "github.com/weissmall/smallctl/internal/general"

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
