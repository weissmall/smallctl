package config

const (
	DefaultTimeout = 30
)

const (
	DefaultShell  = "bash"
	DefaultNotify = "off"
)

var ValidNotifyValues = map[string]bool{"off": true, "error": true, "all": true}
