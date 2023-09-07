package config

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"

	"github.com/autobrr/autobrr/pkg/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var configTemplate = `# config.toml

# Hostname / IP
#
# Default: "0.0.0.0"
#
host = "{{ .host }}"

# Port
#
# Default: 42069
#
port = 42069

# Pre Import Path of qBittorrent for Sonarr
# Needs to be filled out correctly, e.g. "/data/torrents/tv-hd"
#
# Default: ""
#
preImportPath = ""

# seasonpackarr logs file
# If not defined, logs to stdout
# Make sure to use forward slashes and include the filename with extension. eg: "logs/seasonpackarr.log", "C:/seasonpackarr/logs/seasonpackarr.log"
#
# Optional
#
#logPath = "logs/seasonpackarr.log"

# Log level
#
# Default: "DEBUG"
#
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
#
logLevel = "DEBUG"

# Log Max Size
#
# Default: 50
#
# Max log size in megabytes
#
#logMaxSize = 50

# Log Max Backups
#
# Default: 3
#
# Max amount of old log files
#
#logMaxBackups = 3
`

func (c *AppConfig) writeConfig(configPath string, configFile string) error {
	cfgPath := filepath.Join(configPath, configFile)

	// check if configPath exists, if not create it
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(configPath, os.ModePerm)
		if err != nil {
			log.Println(err)
			return err
		}
	}

	// check if config exists, if not create it
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		// set default host
		host := "0.0.0.0"

		if _, err := os.Stat("/.dockerenv"); err == nil {
			// docker creates a .dockerenv file at the root
			// of the directory tree inside the container.
			// if this file exists then the viewer is running
			// from inside a docker container so return true
			host = "0.0.0.0"
		} else if _, err := os.Stat("/dev/.lxc-boot-id"); err == nil {
			// lxc creates this file containing the uuid
			// of the container in every boot.
			// if this file exists then the viewer is running
			// from inside a lxc container so return true
			host = "0.0.0.0"
		} else if pd, _ := os.Open("/proc/1/cgroup"); pd != nil {
			defer pd.Close()
			b := make([]byte, 4096)
			pd.Read(b)
			if strings.Contains(string(b), "/docker") || strings.Contains(string(b), "/lxc") {
				host = "0.0.0.0"
			}
		}

		f, err := os.Create(cfgPath)
		if err != nil { // perm 0666
			// handle failed create
			log.Printf("error creating file: %q", err)
			return err
		}
		defer f.Close()

		// setup text template to inject variables into
		tmpl, err := template.New("config").Parse(configTemplate)
		if err != nil {
			return errors.Wrap(err, "could not create config template")
		}

		tmplVars := map[string]string{
			"host": host,
		}

		var buffer bytes.Buffer
		if err = tmpl.Execute(&buffer, &tmplVars); err != nil {
			return errors.Wrap(err, "could not write template output")
		}

		if _, err = f.WriteString(buffer.String()); err != nil {
			log.Printf("error writing contents to file: %v %q", configPath, err)
			return err
		}

		return f.Sync()
	}

	return nil
}

type Config interface {
	UpdateConfig() error
	DynamicReload(log logger.Logger)
}

type AppConfig struct {
	Config *domain.Config
	m      sync.Mutex
}

func New(configPath string, version string) *AppConfig {
	c := &AppConfig{}
	c.defaults()
	c.Config.Version = version
	c.Config.ConfigPath = configPath

	c.load(configPath)

	return c
}

func (c *AppConfig) defaults() {
	c.Config = &domain.Config{
		Version:       "dev",
		Host:          "0.0.0.0",
		Port:          42069,
		PreImportPath: "",
		LogLevel:      "DEBUG",
		LogPath:       "",
		LogMaxSize:    50,
		LogMaxBackups: 3,
	}

}

func (c *AppConfig) load(configPath string) {
	// or use viper.SetDefault(val, def)
	//viper.SetDefault("host", config.Host)
	//viper.SetDefault("port", config.Port)
	//viper.SetDefault("logLevel", config.LogLevel)
	//viper.SetDefault("logPath", config.LogPath)

	viper.SetConfigType("toml")

	// clean trailing slash from configPath
	configPath = path.Clean(configPath)
	if configPath != "" {
		//viper.SetConfigName("config")

		// check if path and file exists
		// if not, create path and file
		if err := c.writeConfig(configPath, "config.toml"); err != nil {
			log.Printf("write error: %q", err)
		}

		viper.SetConfigFile(path.Join(configPath, "config.toml"))
	} else {
		viper.SetConfigName("config")

		// Search config in directories
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/seasonpackarr")
		viper.AddConfigPath("$HOME/.seasonpackarr")
	}

	viper.SetEnvPrefix("SEASONPACKARR")

	// read config
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("config read error: %q", err)
	}

	if viper.GetString("preImportPath") == "" {
		log.Fatal("preImportPath can't be empty, please provide a valid path to the directory you want seasonpacks to be hardlinked to")
	}

	if _, err := os.Stat(viper.GetString("preImportPath")); os.IsNotExist(err) {
		log.Fatal("preImportPath doesn't exist, please make sure you entered the correct path")
	}

	for _, key := range viper.AllKeys() {
		envKey := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		err := viper.BindEnv(key, "SEASONPACKARR__"+envKey)
		if err != nil {
			log.Fatal("config: unable to bind env: " + err.Error())
		}
	}

	if err := viper.Unmarshal(c.Config); err != nil {
		log.Fatalf("Could not unmarshal config file: %v: err %q", viper.ConfigFileUsed(), err)
	}
}

func (c *AppConfig) DynamicReload(log logger.Logger) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		c.m.Lock()

		preImportPath := viper.GetString("preImportPath")
		c.Config.PreImportPath = preImportPath

		logLevel := viper.GetString("logLevel")
		c.Config.LogLevel = logLevel
		log.SetLogLevel(c.Config.LogLevel)

		logPath := viper.GetString("logPath")
		c.Config.LogPath = logPath

		log.Debug().Msg("config file reloaded!")

		c.m.Unlock()
	})
	viper.WatchConfig()

	return
}

func (c *AppConfig) UpdateConfig() error {
	file := path.Join(c.Config.ConfigPath, "config.toml")

	f, err := os.ReadFile(file)
	if err != nil {
		return errors.Wrap(err, "could not read config file: %s", file)
	}

	lines := strings.Split(string(f), "\n")
	lines = c.processLines(lines)

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(file, []byte(output), 0644); err != nil {
		return errors.Wrap(err, "could not write config file: %s", file)
	}

	return nil
}

func (c *AppConfig) processLines(lines []string) []string {
	// keep track of not found values to append at bottom
	var (
		foundLineLogLevel = false
		foundLineLogPath  = false
	)

	for i, line := range lines {
		if !foundLineLogLevel && strings.Contains(line, "logLevel =") {
			lines[i] = fmt.Sprintf(`logLevel = "%s"`, c.Config.LogLevel)
			foundLineLogLevel = true
		}
		if !foundLineLogPath && strings.Contains(line, "logPath =") {
			if c.Config.LogPath == "" {
				lines[i] = `#logPath = ""`
			} else {
				lines[i] = fmt.Sprintf(`logPath = "%s"`, c.Config.LogPath)
			}
			foundLineLogPath = true
		}
	}

	if !foundLineLogLevel {
		lines = append(lines, "# Log level")
		lines = append(lines, "#")
		lines = append(lines, `# Default: "DEBUG"`)
		lines = append(lines, "#")
		lines = append(lines, `# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"`)
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf(`logLevel = "%s"`, c.Config.LogLevel))
	}

	if !foundLineLogPath {
		lines = append(lines, "# Log Path")
		lines = append(lines, "#")
		lines = append(lines, "# Optional")
		lines = append(lines, "#")
		if c.Config.LogPath == "" {
			lines = append(lines, `#logPath = ""`)
		} else {
			lines = append(lines, fmt.Sprintf(`logPath = "%s"`, c.Config.LogPath))
		}
	}

	return lines
}
