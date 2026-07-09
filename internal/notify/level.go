package notify

// Level defines when notifications are sent.
type Level string

const (
	LevelOff   Level = "off"
	LevelError Level = "error"
	LevelAll   Level = "all"
)

func ParseLevel(s string) Level {
	switch s {
	case "error":
		return LevelError
	case "all":
		return LevelAll
	default:
		return LevelOff
	}
}
