package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	bolt "go.etcd.io/bbolt"

	"github.com/complytime/gemara-content-service/cmd/compass/server"
	"github.com/complytime/gemara-content-service/internal/logging"
	"github.com/complytime/gemara-content-service/internal/oci"
	compass "github.com/complytime/gemara-content-service/service"
)

func main() {

	var (
		port, catalogPath, configPath string
		storageRoot                   string
		logLevel                      string
		skipTLS                       bool
	)

	flag.StringVar(&port, "port", "8080", "Port for HTTP server")
	flag.BoolVar(&skipTLS, "skip-tls", false, "Run without TLS")
	flag.StringVar(&logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	flag.StringVar(&storageRoot, "storage-root", "", "Path to OCI storage root (contains index.db and blobs/)")

	// TODO: This needs to become Layer 3 policy and complete resolution on startup
	flag.StringVar(&catalogPath, "catalog", "./hack/sampledata/osps.yaml", "Path to Layer 2 catalog")
	flag.StringVar(&configPath, "config", "./docs/config.yaml", "Path to compass config file")
	flag.Parse()

	_, err := logging.Init(logLevel)
	if err != nil {
		slog.Error("failed to initialize logging", "err", err)
		os.Exit(1)
	}

	slog.Info("starting compass service",
		slog.String("port", port),
		slog.String("catalog", catalogPath),
		slog.String("config", configPath),
		slog.String("storage_root", storageRoot),
		slog.Bool("skip_tls", skipTLS),
	)

	catalogPath = filepath.Clean(catalogPath)
	scope, err := server.NewScopeFromCatalogPath(catalogPath)
	if err != nil {
		slog.Error("failed to load catalog", "path", catalogPath, "err", err)
		os.Exit(1)
	}

	var cfg server.Config
	configPath = filepath.Clean(configPath)
	content, err := os.ReadFile(configPath)
	if err != nil {
		slog.Error("failed to read config file", "path", configPath, "err", err)
		os.Exit(1)
	}

	err = yaml.Unmarshal(content, &cfg)
	if err != nil {
		slog.Error("failed to parse config file", "path", configPath, "err", err)
		os.Exit(1)
	}

	transformers, err := server.NewMapperSet(&cfg)
	if err != nil {
		slog.Error("failed to initialize plugin mappers", "err", err)
		os.Exit(1)
	}

	service := compass.NewService(transformers, scope)

	// Initialize the OCI registry if a storage root is configured.
	var registry *oci.Registry
	if storageRoot != "" {
		storageRoot = filepath.Clean(storageRoot)
		dbPath := filepath.Join(storageRoot, "index.db")
		blobRoot := filepath.Join(storageRoot, "blobs")

		db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			slog.Error("failed to open OCI index database", "path", dbPath, "err", err)
			os.Exit(1)
		}
		defer db.Close()

		registry = oci.NewRegistry(db, blobRoot)
		slog.Info("OCI registry initialized", "storage_root", storageRoot)
	}

	s := server.NewGinServer(service, registry, port, &cfg)

	if skipTLS {
		slog.Warn("Insecure connections permitted. TLS is highly recommended for production")
		if err := s.ListenAndServe(); err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	} else {
		cert, key := server.SetupTLS(s, cfg)
		if err := s.ListenAndServeTLS(cert, key); err != nil {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}
}
