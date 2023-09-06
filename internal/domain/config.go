package domain

type Config struct {
	Version       string
	ConfigPath    string
	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	PreImportPath string `toml:"preImportPath"`
	LogPath       string `toml:"logPath"`
	LogLevel      string `toml:"logLevel"`
	LogMaxSize    int    `toml:"logMaxSize"`
	LogMaxBackups int    `toml:"logMaxBackups"`
}
