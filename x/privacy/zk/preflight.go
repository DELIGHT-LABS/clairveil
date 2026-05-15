package zk

import (
	"fmt"
	"os"
	"strings"

	"cosmossdk.io/log/v2"
)

const ZKPreflightModeEnv = "CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE"

type ZKPreflightMode string

const (
	ZKPreflightOff    ZKPreflightMode = "off"
	ZKPreflightWarn   ZKPreflightMode = "warn"
	ZKPreflightStrict ZKPreflightMode = "strict"
)

func ParseZKPreflightMode(raw string) (ZKPreflightMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ZKPreflightWarn, nil
	}

	switch ZKPreflightMode(normalized) {
	case ZKPreflightOff, ZKPreflightWarn, ZKPreflightStrict:
		return ZKPreflightMode(normalized), nil
	default:
		return "", fmt.Errorf("invalid privacy zk preflight mode %q", raw)
	}
}

func RunPreflight(logger log.Logger) error {
	mode, err := ParseZKPreflightMode(os.Getenv(ZKPreflightModeEnv))
	if err != nil {
		return err
	}

	if mode == ZKPreflightOff {
		return nil
	}

	if err := ValidateZKArtifacts(); err != nil {
		if mode == ZKPreflightWarn {
			logger.Warn("privacy zk preflight failed", "mode", mode, "err", err)
			return nil
		}

		return fmt.Errorf("privacy zk preflight failed: %w", err)
	}

	logger.Info("privacy zk preflight passed", "mode", mode, "artifact_dir", artifactDir())
	return nil
}
