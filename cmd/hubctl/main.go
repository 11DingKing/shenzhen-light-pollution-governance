package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/benbjohnson/clock"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/service"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "data directory")
	configPath := fs.String("config", "", "config file path")
	_ = fs.Parse(os.Args[2:])

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if *dataDir != "" {
		cfg.Storage.DataDir = *dataDir
	}

	logger := applog.New(cfg.Logging.Level, cfg.Logging.Format)
	clk := clock.New()
	ctx := context.Background()

	switch cmd {
	case "init":
		runInit(ctx, cfg, clk, logger)
	case "import":
		runImport(ctx, cfg, clk, logger, fs)
	case "export":
		runExport(ctx, cfg, clk, logger, fs)
	case "reconcile":
		runReconcile(ctx, cfg, logger)
	case "rebuild-index":
		runRebuildIndex(ctx, cfg, logger)
	case "diagnose":
		runDiagnose(ctx, cfg, logger)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `hubctl - operations CLI for lead responsibility adjudication service

Usage: hubctl <command> [flags]

Commands:
  init           Initialize data directory and schema
  import         Import items from a JSON file
  export         Export items to a JSON file
  reconcile      Reconcile shard files with index
  rebuild-index  Rebuild SQLite index from shard files
  diagnose       Diagnose system status`)
}

func openStore(ctx context.Context, cfg *config.Config, clk clock.Clock) (*repo.Store, error) {
	st, err := repo.New(ctx, cfg.Storage.DataDir, clk, cfg.Storage.ShardMaxSize, cfg.Storage.SyncOnWrite)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return st, nil
}

func runInit(ctx context.Context, cfg *config.Config, clk clock.Clock, logger *applog.Logger) {
	st, err := openStore(ctx, cfg, clk)
	if err != nil {
		logger.Fatal().Err(err).Msg("init failed")
	}
	defer st.Close()

	adj := dispatch.NewAdjudicator(clk)
	ruleSvc := service.NewRuleService(st, clk)

	rule := &service.CreateRuleRequest{
		Name:           "default-dispatch",
		Description:    "default fallback rule for unclassified items",
		LeadDepartment: "general-affairs-bureau",
		Priority:       0,
		IsDefault:      true,
		CreatedBy:      "init",
	}
	created, err := ruleSvc.CreateRule(ctx, *rule)
	if err != nil {
		logger.Fatal().Err(err).Msg("create default rule failed")
	}
	logger.Info().Str("rule_version", fmt.Sprintf("%d", created.Version)).Msg("default rule created")
	fmt.Println("init complete")
	_ = adj
}

func runImport(ctx context.Context, cfg *config.Config, clk clock.Clock, logger *applog.Logger, fs *flag.FlagSet) {
	filePath := fs.String("file", "", "input JSON file")
	_ = fs.Parse(os.Args[2:])
	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "error: --file is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(*filePath)
	if err != nil {
		logger.Fatal().Err(err).Msg("read input file")
	}
	var req service.BatchImportRequest
	if err := json.Unmarshal(data, &req); err != nil {
		logger.Fatal().Err(err).Msg("parse input JSON")
	}

	st, err := openStore(ctx, cfg, clk)
	if err != nil {
		logger.Fatal().Err(err).Msg("open store")
	}
	defer st.Close()

	adj := dispatch.NewAdjudicator(clk)
	itemSvc := service.NewItemService(st, adj, clk, cfg.Business.DefaultDeadline)
	batchSvc := service.NewBatchService(st, itemSvc, clk)

	result, err := batchSvc.Import(ctx, req)
	if err != nil {
		logger.Fatal().Err(err).Msg("import failed")
	}
	fmt.Printf("import complete: %d success, %d failure\n", result.SuccessCount, result.FailureCount)
	for _, r := range result.Results {
		if !r.Success {
			fmt.Fprintf(os.Stderr, "  row %d: %s - %s\n", r.RowIndex, r.ExternalRef, r.Error)
		}
	}
}

func runExport(ctx context.Context, cfg *config.Config, clk clock.Clock, logger *applog.Logger, fs *flag.FlagSet) {
	fromStr := fs.String("from", "", "start date (RFC3339)")
	toStr := fs.String("to", "", "end date (RFC3339)")
	outPath := fs.String("out", "", "output file path")
	_ = fs.Parse(os.Args[2:])

	st, err := openStore(ctx, cfg, clk)
	if err != nil {
		logger.Fatal().Err(err).Msg("open store")
	}
	defer st.Close()

	adj := dispatch.NewAdjudicator(clk)
	itemSvc := service.NewItemService(st, adj, clk, cfg.Business.DefaultDeadline)
	batchSvc := service.NewBatchService(st, itemSvc, clk)

	req := service.BatchExportRequest{}
	if *fromStr != "" {
		if t, err := time.Parse(time.RFC3339, *fromStr); err == nil {
			req.From = t
		}
	}
	if *toStr != "" {
		if t, err := time.Parse(time.RFC3339, *toStr); err == nil {
			req.To = t
		}
	}
	items, total, err := batchSvc.Export(ctx, req)
	if err != nil {
		logger.Fatal().Err(err).Msg("export failed")
	}

	var out *os.File = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			logger.Fatal().Err(err).Msg("create output file")
		}
		defer f.Close()
		out = f
	}
	for _, item := range items {
		data, _ := json.Marshal(item)
		fmt.Fprintln(out, string(data))
	}
	fmt.Fprintf(os.Stderr, "export complete: %d items\n", total)
}

func runReconcile(ctx context.Context, cfg *config.Config, logger *applog.Logger) {
	st, err := openStore(ctx, cfg, clock.New())
	if err != nil {
		logger.Fatal().Err(err).Msg("open store")
	}
	defer st.Close()

	report, err := st.Reconcile(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("reconcile failed")
	}
	fmt.Printf("reconcile complete:\n")
	fmt.Printf("  shard count: %d\n", report.ShardCount)
	fmt.Printf("  index count: %d\n", report.IndexCount)
	fmt.Printf("  orphaned in shard: %d\n", report.OrphanedInShard)
	fmt.Printf("  missing in shard: %d\n", report.MissingInShard)
	fmt.Printf("  checksum mismatches: %d\n", report.ChecksumMismatches)
	for _, d := range report.Details {
		fmt.Printf("  - %s\n", d)
	}
}

func runRebuildIndex(ctx context.Context, cfg *config.Config, logger *applog.Logger) {
	st, err := openStore(ctx, cfg, clock.New())
	if err != nil {
		logger.Fatal().Err(err).Msg("open store")
	}
	defer st.Close()

	report, err := st.RebuildIndex(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("rebuild index failed")
	}
	fmt.Printf("rebuild-index complete:\n")
	fmt.Printf("  total shards: %d\n", report.TotalShards)
	fmt.Printf("  indexed shards: %d\n", report.IndexedShards)
	fmt.Printf("  skipped shards: %d\n", report.SkippedShards)
	fmt.Printf("  total records: %d\n", report.TotalRecords)
	for _, c := range report.CorruptedShards {
		fmt.Printf("  corrupted: %s - %s\n", c.Path, c.Reason)
	}
}

func runDiagnose(ctx context.Context, cfg *config.Config, logger *applog.Logger) {
	st, err := openStore(ctx, cfg, clock.New())
	if err != nil {
		logger.Fatal().Err(err).Msg("open store")
	}
	defer st.Close()

	report, err := st.Diagnose(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("diagnose failed")
	}
	fmt.Printf("diagnose complete:\n")
	fmt.Printf("  data dir writable: %v\n", report.DataDirWritable)
	fmt.Printf("  item count: %d\n", report.ItemCount)
	fmt.Printf("  rule count: %d\n", report.RuleCount)
	fmt.Printf("  overdue count: %d\n", report.OverdueCount)
	fmt.Printf("  corrupted shards: %d\n", report.CorruptedShards)
	fmt.Printf("  schema version: %d\n", report.SchemaVersion)
	fmt.Printf("  ready: %v\n", report.Ready)
	for _, issue := range report.Issues {
		fmt.Printf("  issue: %s\n", issue)
	}
	for _, m := range report.ShardManifest {
		fmt.Printf("  shard: %s type=%s count=%d status=%s\n", m.ShardPath, m.EntityType, m.RecordCount, m.Status)
	}
}
