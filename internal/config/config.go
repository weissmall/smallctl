package config

// Config is the root of the configuration tree.
// It is always kept in RAM; the watcher re-parses and swaps it atomically.
type Config struct {
	// Options holds global server settings.
	Options Options `yaml:"options"`

	// Commands maps command names to their definitions.
	Commands map[string]Command `yaml:"commands"`
}
