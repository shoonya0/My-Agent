// Command migrate runs MySQL schema migrations using golang-migrate.
//
// Environment (either works):
//   - DATABASE_URL: mysql://user:pass@tcp(host:3306)/dbname?multiStatements=true
//   - MYSQL_DSN:    go-sql-driver form, e.g. user:pass@tcp(127.0.0.1:3306)/myagent?parseTime=true
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	log.SetFlags(0)

	var (
		cmd        = flag.String("cmd", "", "one of: up, down, version, force, create (required)")
		steps      = flag.Uint("steps", 0, "for up/down: max migrations to apply (0 = all for up, 1 for down default in migrate)")
		pathFlag   = flag.String("path", "migrations", "directory of .up.sql / .down.sql files")
		dbURL      = flag.String("database", "", "override DATABASE_URL / MYSQL_DSN")
		forceVer   = flag.Int("version", -1, "for force: set schema_migrations version (destructive; read migrate docs)")
		createName = flag.String("name", "", "for create: migration name suffix, e.g. add_column_x")
	)
	flag.Parse()

	if *cmd == "" {
		flag.Usage()
		os.Exit(2)
	}

	migrationsDir, err := filepath.Abs(*pathFlag)
	if err != nil {
		log.Fatalf("migrations path: %v", err)
	}
	sourceURL := fileSourceURL(migrationsDir)

	dbStr, err := resolveDatabaseURL(*dbURL)
	if err != nil {
		log.Fatalf("database URL: %v", err)
	}

	m, err := migrate.New(sourceURL, dbStr)
	if err != nil {
		log.Fatalf("migrate init: %v", err)
	}
	defer m.Close()

	switch *cmd {
	case "up":
		if *steps == 0 {
			err = m.Up()
		} else {
			err = m.Steps(int(*steps))
		}
		if errors.Is(err, migrate.ErrNoChange) {
			log.Println("no change (already at latest)")
			return
		}
	case "down":
		n := int(*steps)
		if n == 0 {
			n = 1
		}
		err = m.Steps(-n)
		if errors.Is(err, migrate.ErrNoChange) {
			log.Println("no change (already at baseline)")
			return
		}
	case "version":
		v, dirty, verr := m.Version()
		if verr != nil && !errors.Is(verr, migrate.ErrNilVersion) {
			log.Fatalf("version: %v", verr)
		}
		if errors.Is(verr, migrate.ErrNilVersion) {
			log.Println("version: nil (no migrations applied)")
			return
		}
		log.Printf("version: %d dirty=%v", v, dirty)
		return
	case "force":
		if *forceVer < 0 {
			log.Fatal("force requires -version N")
		}
		err = m.Force(*forceVer)
	case "create":
		if *createName == "" {
			log.Fatal("create requires -name")
		}
		err = createMigrationFiles(migrationsDir, *createName)
	default:
		log.Fatalf("unknown -cmd %q", *cmd)
	}

	if err != nil {
		log.Fatalf("%s: %v", *cmd, err)
	}
	log.Printf("%s: ok", *cmd)
}

// fileSourceURL builds a RFC 8089 file URI golang-migrate accepts on Unix and Windows.
func fileSourceURL(abs string) string {
	p := filepath.ToSlash(abs)
	if len(p) >= 2 && p[1] == ':' {
		// Windows: file:///D:/path
		return "file:///" + p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func resolveDatabaseURL(override string) (string, error) {
	if override != "" {
		return ensureMigrateMySQLURL(override)
	}
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return ensureMigrateMySQLURL(u)
	}
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		return goMySQLDSNToMigrateURL(dsn)
	}
	return "", fmt.Errorf("set DATABASE_URL or MYSQL_DSN, or pass -database")
}

func ensureMigrateMySQLURL(s string) (string, error) {
	if strings.HasPrefix(s, "mysql://") {
		return s, nil
	}
	return goMySQLDSNToMigrateURL(s)
}

// goMySQLDSNToMigrateURL converts a go-sql-driver/mysql DSN to golang-migrate's mysql URL form.
func goMySQLDSNToMigrateURL(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse MYSQL_DSN: %w", err)
	}
	if cfg.DBName == "" {
		return "", fmt.Errorf("MYSQL_DSN must include database name")
	}

	u := url.URL{
		Scheme: "mysql",
		Path:   "/" + cfg.DBName,
	}
	if cfg.User != "" {
		u.User = url.UserPassword(cfg.User, cfg.Passwd)
	}
	u.Host = "tcp(" + cfg.Addr + ")"

	q := url.Values{}
	for k, v := range cfg.Params {
		q.Set(k, v)
	}
	if q.Get("multiStatements") == "" {
		q.Set("multiStatements", "true")
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func createMigrationFiles(dir, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	maxVer := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		idx := strings.IndexByte(base, '_')
		if idx <= 0 {
			continue
		}
		v, err := strconv.Atoi(base[:idx])
		if err != nil {
			continue
		}
		if v > maxVer {
			maxVer = v
		}
	}
	next := maxVer + 1
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, name)
	safe = strings.Trim(safe, "_")
	if safe == "" {
		return fmt.Errorf("invalid -name")
	}
	prefix := fmt.Sprintf("%03d_%s", next, safe)
	paths := []string{
		filepath.Join(dir, prefix+".up.sql"),
		filepath.Join(dir, prefix+".down.sql"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("file already exists: %s", p)
		}
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("-- TODO\n"), 0o644); err != nil {
			return err
		}
	}
	log.Printf("created %s and .down.sql", paths[0])
	return nil
}
