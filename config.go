package torrentd

import (
	"fmt"

	_ "embed"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Client ClientConfig `toml:"client"`
	Web    WebConfig    `toml:"web"`
	Debug  DebugConfig
}

type ClientConfig struct {
	Bind         string `toml:"bind"`
	EnableUpnp   bool   `toml:"enable_upnp_forwarding"`
	DownloadPath string `toml:"download_path"`
	StatePath    string `toml:"state_path"`
	TorrentPath  string `toml:"torrent_path"` // TODO
}

type WebConfig struct {
	Bind        string `toml:"bind"`
	TLSCertFile string `toml:"tls_cert"`
	TLSKeyFile  string `toml:"tls_key"`
}

type DebugConfig struct {
	BindPProf             string `toml:"bind_pprof"`
	AnacrolixDebugLogging bool   `toml:"enable_anacrolix_debug_logging"`
}

//go:embed torrentd.default.conf
var defaultConfBytes []byte

var defaultConf *Config

func init() {
	err := toml.Unmarshal(defaultConfBytes, &defaultConf)
	if err != nil {
		panic(fmt.Errorf("loading default config: %w", err))
	}
}

func DefaultConfig() *Config {
	return defaultConf
}
