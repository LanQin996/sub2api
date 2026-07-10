package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	defaultOpenAICodexOriginator = "codex_cli_rs"
	defaultOpenAICodexVersion    = "0.144.1"
	defaultOpenAICodexUserAgent  = defaultOpenAICodexOriginator + "/" + defaultOpenAICodexVersion + " (Ubuntu 24.04.2; x86_64) xterm-256color"
)

type OpenAICodexFingerprint struct {
	UserAgent  string
	Originator string
	Version    string
}

func OpenAICodexFingerprintFromConfig(cfg *config.Config) OpenAICodexFingerprint {
	fp := OpenAICodexFingerprint{
		UserAgent:  defaultOpenAICodexUserAgent,
		Originator: defaultOpenAICodexOriginator,
		Version:    defaultOpenAICodexVersion,
	}
	if cfg == nil {
		return fp
	}
	overlay := cfg.Gateway.OpenAICodexFingerprint
	if v := strings.TrimSpace(overlay.UserAgent); v != "" {
		fp.UserAgent = v
	}
	if v := strings.TrimSpace(overlay.Originator); v != "" {
		fp.Originator = v
	}
	if v := strings.TrimSpace(overlay.Version); v != "" {
		fp.Version = v
	}
	return fp
}
