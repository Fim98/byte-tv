package app

import (
	"errors"
	"os"
)

const userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/604.1"

type Config struct {
	adminUsername         string
	adminPassword         string
	adminUserExplicit     bool
	adminPasswordExplicit bool
	settingsLoaded        bool
	dataDir               string
	outputDirSetting      string
	OutputDir             string `json:"outputDir"`
	GroupBySource         bool   `json:"groupBySource"`
	FFmpeg                string `json:"ffmpeg"`
	Concurrency           int    `json:"concurrency"`
	RequestConcurrency    int    `json:"requestConcurrency"`
	RequestIntervalMS     int    `json:"requestIntervalMs"`
	MaxPagesPerSort       int    `json:"maxPagesPerSort"`
	Retries               int    `json:"retries"`
	SkipBytes             int64  `json:"skipBytes"`
	InsecureTLS           bool   `json:"insecureTLS"`
	ProxyURL              string `json:"proxyURL,omitempty"`
	HongguoURL            string `json:"hongguoURL,omitempty"`
}

func defaultConfig() Config {
	return Config{
		OutputDir: defaultOutputDir(), FFmpeg: "ffmpeg", Concurrency: 2,
		RequestConcurrency: 2, RequestIntervalMS: 500, MaxPagesPerSort: 50,
		Retries: 3, SkipBytes: 512 * 1024,
	}
}

func defaultOutputDir() string { return "短剧下载" }

func applyConfigEnvironment(config *Config) {
	for variable, target := range map[string]*string{
		"JUKU_HONGGUO_URL": &config.HongguoURL,
		"JUKU_PROXY_URL":   &config.ProxyURL,
		"JUKU_OUTPUT_DIR":  &config.OutputDir,
		"JUKU_FFMPEG":      &config.FFmpeg,
	} {
		if value := os.Getenv(variable); value != "" {
			*target = value
		}
	}
}

func (config Config) validate() error {
	if _, err := configuredProxy(config.ProxyURL); err != nil {
		return err
	}
	if config.HongguoURL != "" && !isProviderHTTPMediaURL(config.HongguoURL) {
		return errors.New("红果站点地址必须是有效的 HTTP/HTTPS URL")
	}
	return nil
}
