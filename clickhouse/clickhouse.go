// Package clickhouse contains useful clickhouse integration test logic.
package clickhouse

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/clickhouse"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// CHCluster is a single-node CH cluster for testing, setup using docker container.
// To get connection string to CH in this cluster use DSN() method.
// Note: this cluster is setup without zookeeper, so make sure you have ClickHouse
// Keeper section in your clickhouse config.
type CHCluster struct {
	ch testcontainers.Container

	chEndpoint string
}

// CHClusterConfig is used to setup CH cluster for testing.
type CHClusterConfig struct {
	// Clickhouse image version, e.g. "22.9.2.7".
	Version string
	// Path on host to the test configuration. Will
	// be mounted as /etc/clickhouse-server/config.xml.
	CHConfigPath string
	// BindPaths contain optional paths to extra files
	// that will be mounted under /etc/clickhouse-server.
	BindPaths []string
	// Migrations contains migrations that will be
	// applied to the CH contains.
	Migrations []CHMigration
	// ImportData will be imported into the cluster
	// after migrations.
	ImportData []CHData
}

// CHMigration is used to migrate CH.
type CHMigration struct {
	Replacements map[string]string
	// Path to migrations directory on host.
	Path   string
	DBName string
}

// CHData is used to import some test data into CHCluster.
type CHData struct {
	SourceTSV   string
	DBName      string
	TargetTable string
}

// NewCHCluster brings up test CH cluster that can be used in integration tests.
// All resources are cleaned up if a cluster could not be setup. However, it is
// a caller's responsibility to call TearDown once done working with a valid cluster.
func NewCHCluster(ctx context.Context, config *CHClusterConfig) (CHCluster, error) {
	const chConfigBasePath = "/etc/clickhouse-server"

	var (
		cluster CHCluster
		err     error
	)

	binds := testcontainers.ContainerMounts{
		testcontainers.BindMount(
			config.CHConfigPath,
			testcontainers.ContainerMountTarget(filepath.Join(chConfigBasePath, "config.xml")),
		),
	}

	for _, path := range config.BindPaths {
		binds = append(
			binds,
			testcontainers.BindMount(
				path,
				testcontainers.ContainerMountTarget(filepath.Join(chConfigBasePath, filepath.Base(path))),
			),
		)
	}

	cluster.ch, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "clickhouse/clickhouse-server:" + config.Version,
			ExposedPorts: []string{"9000", "8123"},
			Mounts:       binds,
			WaitingFor:   wait.ForListeningPort("9000"),
			Hostname:     "clickhouse",
		},
		Started:      true,
		ProviderType: testcontainers.ProviderDocker,
	})
	if err != nil {
		return CHCluster{}, fmt.Errorf("failed to create CH container: %w", err)
	}

	cluster.chEndpoint, err = cluster.ch.PortEndpoint(ctx, "9000", "clickhouse")
	if err != nil {
		cluster.TearDown(ctx)
		return CHCluster{}, fmt.Errorf("failed to get CH endpoint: %w", err)
	}

	for i := range config.Migrations {
		if err := cluster.applyMigrations(ctx, config.Migrations[i]); err != nil {
			cluster.TearDown(ctx)
			return CHCluster{}, fmt.Errorf("failed to apply CH migrations for %s: %w", config.Migrations[i].DBName, err)
		}
	}

	if err := cluster.fill(ctx, config.ImportData); err != nil {
		cluster.TearDown(ctx)
		return CHCluster{}, fmt.Errorf("failed to fill CH with test data: %w", err)
	}

	return cluster, nil
}

// TearDown terminates CHCluster logging any errors occurred during cleanup.
func (c CHCluster) TearDown(ctx context.Context) {
	if c.ch != nil {
		if err := c.ch.Terminate(ctx); err != nil {
			log.Printf("could not terminate CH container: %v", err)
		}
	}
}

// DSN returns a connection string that can be used to connect to CH in the cluster.
// Returned dsn is "clickhouse://host:port/dbName".
func (c CHCluster) DSN(dbName string) string {
	return fmt.Sprintf("%s/%s", c.chEndpoint, dbName)
}

func (c CHCluster) applyMigrations(ctx context.Context, migration CHMigration) error {
	cmd := exec.CommandContext(ctx, //nolint:gosec
		"docker", "exec", "-i", c.ch.GetContainerID(),
		"clickhouse", "client",
		"--query", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", migration.DBName))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run %q: %w", cmd.Args, err)
	}

	migrations, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("failed to create tmp migrations dir: %w", err)
	}
	defer os.RemoveAll(migrations) //nolint:errcheck

	if err := cpMigrations(migration.Path, migrations, migration.Replacements); err != nil {
		return fmt.Errorf("failed to copy migrations: %w", err)
	}

	m, err := migrate.New(
		fmt.Sprintf("file://%s", migrations),
		c.DSN(migration.DBName)+"?x-multi-statement=true",
	)
	if err != nil {
		return fmt.Errorf("failed to create CH migrator: %w", err)
	}
	defer m.Close() //nolint:errcheck

	if err := m.Up(); err != nil {
		return fmt.Errorf("failed to migrate CH: %w", err)
	}

	version, _, _ := m.Version()
	log.Printf("migration success, version %v", version)

	return nil
}

func (c CHCluster) fill(ctx context.Context, data []CHData) error {
	importTSV := func(tsvPath, dbName, table string) error {
		if tsvPath == "" {
			return nil
		}

		tsv, err := os.Open(tsvPath) //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", tsvPath, err)
		}
		defer tsv.Close() //nolint:errcheck,gosec

		cmd := exec.CommandContext(ctx, //nolint:gosec
			"docker", "exec", "-i", c.ch.GetContainerID(),
			"clickhouse", "client", "--multiquery", "--multiline", "--database", dbName,
			"--query", fmt.Sprintf("INSERT INTO %s FORMAT TSVWithNames", table))
		cmd.Stdin = tsv
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run %q: %w", cmd.Args, err)
		}
		return nil
	}

	for i := range data {
		if err := importTSV(data[i].SourceTSV, data[i].DBName, data[i].TargetTable); err != nil {
			return fmt.Errorf("failed to import %s into %s: %w", data[i].SourceTSV, data[i].TargetTable, err)
		}
	}
	return nil
}

// cpMigrations copies clickhouse migrations substituting {{ cluster }}
// and {{ database }} patterns on-the-fly. Target directory should exist.
// cpMigrations is not recursive, i.e. only regular files will copied while any
// inner directories will be skipped.
func cpMigrations(from, to string, replacements map[string]string) error {
	ff, err := os.ReadDir(from)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", from, err)
	}

	replPairs := make([]string, 0, len(replacements)*2)
	for k, v := range replacements {
		replPairs = append(replPairs, k, v)
	}
	r := strings.NewReplacer(replPairs...)

	cpWithReplace := func(from, to string) error {
		src, err := os.ReadFile(from) //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", from, err)
		}

		tgtFile, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", to, err)
		}
		defer tgtFile.Close() //nolint:errcheck,gosec

		if _, err := r.WriteString(tgtFile, string(src)); err != nil {
			return fmt.Errorf("failed to write %s: %w", from, err)
		}
		return nil
	}

	for _, f := range ff {
		if !f.Type().IsRegular() {
			continue
		}

		srcName := filepath.Join(from, f.Name())
		tgtName := filepath.Join(to, f.Name())

		if err := cpWithReplace(srcName, tgtName); err != nil {
			return fmt.Errorf("failed to write %s: %w", srcName, err)
		}
	}

	return nil
}
