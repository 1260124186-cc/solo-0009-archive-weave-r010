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
	Address           string
	DataPath          string
	AuditPath         string
	SnapshotPath      string
	ComparisonPath    string
	ReadOnly          bool
	ShutdownTimeout   time.Duration
	ComparisonWorkers int
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
	snapshotPath := flags.String("snapshots", defaults.SnapshotPath, "JSON version snapshot path")
	comparisonPath := flags.String("comparisons", defaults.ComparisonPath, "JSON comparison job path")
	readOnly := flags.Bool("read-only", defaults.ReadOnly, "serve without mutating endpoints")
	shutdownTimeout := flags.Duration("shutdown-timeout", defaults.ShutdownTimeout, "graceful shutdown timeout")
	comparisonWorkers := flags.Int("comparison-workers", defaults.ComparisonWorkers, "maximum concurrent comparison items")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Address:           strings.TrimSpace(*address),
		DataPath:          strings.TrimSpace(*dataPath),
		AuditPath:         strings.TrimSpace(*auditPath),
		SnapshotPath:      strings.TrimSpace(*snapshotPath),
		ComparisonPath:    strings.TrimSpace(*comparisonPath),
		ReadOnly:          *readOnly,
		ShutdownTimeout:   *shutdownTimeout,
		ComparisonWorkers: *comparisonWorkers,
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
	if strings.TrimSpace(c.DataPath) == "" {
		return fmt.Errorf("data path cannot be empty")
	}
	if strings.TrimSpace(c.AuditPath) == "" {
		return fmt.Errorf("audit path cannot be empty")
	}
	if strings.TrimSpace(c.SnapshotPath) == "" {
		return fmt.Errorf("snapshot path cannot be empty")
	}
	if strings.TrimSpace(c.ComparisonPath) == "" {
		return fmt.Errorf("comparison path cannot be empty")
	}
	if c.DataPath == c.AuditPath || c.DataPath == c.SnapshotPath || c.DataPath == c.ComparisonPath ||
		c.AuditPath == c.SnapshotPath || c.AuditPath == c.ComparisonPath || c.SnapshotPath == c.ComparisonPath {
		return fmt.Errorf("data, audit, snapshot and comparison paths must be different")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	if c.ComparisonWorkers < 1 || c.ComparisonWorkers > 64 {
		return fmt.Errorf("comparison workers must be between 1 and 64")
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
	workers, err := strconv.Atoi(envOr("ARCHIVE_WEAVE_COMPARISON_WORKERS", "2"))
	if err != nil {
		return Config{}, fmt.Errorf("parse ARCHIVE_WEAVE_COMPARISON_WORKERS: %w", err)
	}
	return Config{
		Address:           envOr("ARCHIVE_WEAVE_ADDR", ":8080"),
		DataPath:          envOr("ARCHIVE_WEAVE_DATA", "./archive-weave-data.json"),
		AuditPath:         envOr("ARCHIVE_WEAVE_AUDIT", "./archive-weave-history.json"),
		SnapshotPath:      envOr("ARCHIVE_WEAVE_SNAPSHOTS", "./archive-weave-snapshots.json"),
		ComparisonPath:    envOr("ARCHIVE_WEAVE_COMPARISONS", "./archive-weave-comparisons.json"),
		ReadOnly:          readOnly,
		ShutdownTimeout:   shutdownTimeout,
		ComparisonWorkers: workers,
	}, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
