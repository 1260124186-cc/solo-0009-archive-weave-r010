package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address              string
	DataPath             string
	AuditPath            string
	SnapshotPath         string
	ComparisonPath       string
	ReadOnly             bool
	ShutdownTimeout      time.Duration
	ComparisonConcurrent int
}

func Load(args []string) (Config, error) {
	defaults, err := defaultConfig()
	if err != nil {
		return Config{}, err
	}
	flags := flag.NewFlagSet("archiveweave", flag.ContinueOnError)
	address := flags.String("addr", defaults.Address, "HTTP listen address")
	dataPath := flags.String("data", defaults.DataPath, "JSON artifact path")
	auditPath := flags.String("audit", defaults.AuditPath, "JSON audit trail path")
	snapshotPath := flags.String("snapshots", defaults.SnapshotPath, "JSON artifact version snapshot path")
	comparisonPath := flags.String("comparisons", defaults.ComparisonPath, "JSON comparison job path")
	readOnly := flags.Bool("read-only", defaults.ReadOnly, "serve without mutating endpoints")
	shutdownTimeout := flags.Duration("shutdown-timeout", defaults.ShutdownTimeout, "graceful shutdown timeout")
	comparisonConcurrent := flags.Int("comparison-concurrency", defaults.ComparisonConcurrent, "maximum parallel comparison items")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Address:              strings.TrimSpace(*address),
		DataPath:             strings.TrimSpace(*dataPath),
		AuditPath:            strings.TrimSpace(*auditPath),
		SnapshotPath:         strings.TrimSpace(*snapshotPath),
		ComparisonPath:       strings.TrimSpace(*comparisonPath),
		ReadOnly:             *readOnly,
		ShutdownTimeout:      *shutdownTimeout,
		ComparisonConcurrent: *comparisonConcurrent,
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return fmt.Errorf("address cannot be empty")
	}
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return fmt.Errorf("address must use host:port form: %w", err)
	}
	for name, path := range map[string]string{
		"data":        c.DataPath,
		"audit":       c.AuditPath,
		"snapshots":   c.SnapshotPath,
		"comparisons": c.ComparisonPath,
	} {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("%s path cannot be empty", name)
		}
	}
	paths := []string{c.DataPath, c.AuditPath, c.SnapshotPath, c.ComparisonPath}
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[i] == paths[j] {
				return fmt.Errorf("data, audit, snapshot and comparison paths must all be different")
			}
		}
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	if c.ComparisonConcurrent <= 0 {
		return fmt.Errorf("comparison concurrency must be positive")
	}
	return nil
}

func defaultConfig() (Config, error) {
	readOnly, err := strconv.ParseBool(envOr("ARCHIVE_WEAVE_READ_ONLY", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_WEAVE_READ_ONLY: %w", err)
	}
	shutdownTimeout, err := time.ParseDuration(envOr("ARCHIVE_WEAVE_SHUTDOWN_TIMEOUT", "5s"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_WEAVE_SHUTDOWN_TIMEOUT: %w", err)
	}
	concurrency, err := strconv.Atoi(envOr("ARCHIVE_WEAVE_COMPARISON_CONCURRENCY", "4"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_WEAVE_COMPARISON_CONCURRENCY: %w", err)
	}
	return Config{
		Address:              envOr("ARCHIVE_WEAVE_ADDR", ":8080"),
		DataPath:             envOr("ARCHIVE_WEAVE_DATA", "./archive-weave-data.json"),
		AuditPath:            envOr("ARCHIVE_WEAVE_AUDIT", "./archive-weave-history.json"),
		SnapshotPath:         envOr("ARCHIVE_WEAVE_SNAPSHOTS", "./archive-weave-snapshots.json"),
		ComparisonPath:       envOr("ARCHIVE_WEAVE_COMPARISONS", "./archive-weave-comparisons.json"),
		ReadOnly:             readOnly,
		ShutdownTimeout:      shutdownTimeout,
		ComparisonConcurrent: concurrency,
	}, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
