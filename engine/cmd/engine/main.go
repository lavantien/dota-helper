// Command engine runs the pick simulator pipeline stages.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"poolguide/internal/builds"
	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/eval"
	"poolguide/internal/gates"
	"poolguide/internal/ingest"
	"poolguide/internal/mine"
	"poolguide/internal/order"
	"poolguide/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfgPath := "config.json"
	if v := os.Getenv("ENGINE_CONFIG"); v != "" {
		cfgPath = v
	}
	// manual scan because flag.Parse stops at the first positional
	refresh := false
	for _, a := range os.Args {
		if a == "-refresh" {
			refresh = true
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	var runErr error
	switch os.Args[1] {
	case "probe":
		runErr = runProbe(arg(2), cfg)
	case "fetch":
		runErr = runFetch(arg(2), cfg, refresh)
	case "ingest":
		runErr = runIngest(cfg)
	case "mine":
		db, err := store.Open(cfg.Paths.DBFile)
		if err != nil {
			runErr = err
		} else {
			runErr = mine.Run(mine.Deps{DB: db, Cfg: cfg, RepoRoot: "."})
			db.Close()
		}
	case "sync":
		runErr = runSync(arg(2), cfg, cfgPath)
	case "db-query":
		runErr = runDBQuery(arg(2), cfg)
	case "emit":
		runErr = runEmit(arg(2), cfg)
	case "eval":
		runErr = runEval(arg(2), arg(3), cfgPath, cfg)
	case "fixtures":
		runErr = notImplemented("fixtures")
	case "verify":
		runErr = notImplemented("verify")
	case "serve":
		runErr = notImplemented("serve")
	default:
		usage()
		os.Exit(2)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
}

func runProbe(what string, cfg *config.Config) error {
	switch what {
	case "stratz":
		return ingest.ProbeStratz(cfg)
	case "opendota":
		return ingest.ProbeOpenDota(cfg)
	case "builds":
		return ingest.ProbeBuilds(cfg)
	case "matches":
		return ingest.ProbeMatches(cfg)
	case "scope":
		return ingest.ProbeScope(cfg)
	case "window":
		return ingest.ProbeWindow(cfg)
	}
	return usageErr("probe " + what)
}

func runFetch(what string, cfg *config.Config, refresh bool) error {
	switch what {
	case "stratz":
		return ingest.FetchStratz(cfg, refresh)
	case "opendota":
		return ingest.FetchOpenDota(cfg)
	case "builds":
		return ingest.FetchBuilds(cfg, refresh)
	case "matches":
		return ingest.FetchStratzMatches(cfg, refresh)
	case "positions":
		return ingest.FetchPositions(cfg)
	}
	return usageErr("fetch " + what)
}

func runIngest(cfg *config.Config) error {
	db, err := store.Open(cfg.Paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	return ingest.Ingest(db, cfg)
}

// runEmit renders the generated artifacts. Bare `emit` runs all three; the
// dated subcommands run one for targeted rebuilds.
func runEmit(what string, cfg *config.Config) error {
	date := time.Now().UTC().Format("2006-01-02")
	steps := map[string]func() error{
		"picker": func() error {
			db, err := store.Open(cfg.Paths.DBFile)
			if err != nil {
				return err
			}
			defer db.Close()
			doc, err := gates.Load(filepath.Join(cfg.Paths.PickerDir, "gates.json"))
			if err != nil {
				return err
			}
			if err := doc.Validate(); err != nil {
				return err
			}
			prose, err := emit.LoadProse(cfg.Paths.ContentPath)
			if err != nil {
				return err
			}
			b, err := emit.Picker(db, cfg, doc, prose, date)
			if err != nil {
				return err
			}
			return emit.WriteFile(cfg.Emit.PickerOut, b)
		},
		"guide": func() error {
			db, err := store.Open(cfg.Paths.DBFile)
			if err != nil {
				return err
			}
			defer db.Close()
			b, err := emit.Guide(db, cfg, date)
			if err != nil {
				return err
			}
			return emit.WriteFile(cfg.Emit.GuideOut, b)
		},
		"data": func() error {
			db, err := store.Open(cfg.Paths.DBFile)
			if err != nil {
				return err
			}
			defer db.Close()
			b, err := emit.DataJSFromFiles(db, "config.json", cfg.Paths.ContentPath)
			if err != nil {
				return err
			}
			return emit.WriteFile(cfg.Emit.DataJsOut, b)
		},
	}
	order := []string{"picker", "guide", "data"}
	if what != "" {
		order = []string{what}
	}
	for _, name := range order {
		step, ok := steps[name]
		if !ok {
			return usageErr("emit " + name)
		}
		if err := step(); err != nil {
			return err
		}
		fmt.Printf("emit %s: wrote from %s\n", name, date)
	}
	return nil
}

func runSync(what string, cfg *config.Config, cfgPath string) error {
	switch what {
	case "builds":
		return builds.Run(cfg)
	case "order":
		db, err := store.Open(cfg.Paths.DBFile)
		if err != nil {
			return err
		}
		defer db.Close()
		return order.Run(order.Deps{DB: db, Cfg: cfg, CfgPath: cfgPath})
	}
	return usageErr("sync " + what)
}

// runEval runs the evaluation stages: report replays the committed drafts
// against the live model, fit derives a proposal into var/, and promote
// applies one behind its guard.
func runEval(what, sub, cfgPath string, cfg *config.Config) error {
	openDB := func() (*sql.DB, error) { return store.Open(cfg.Paths.DBFile) }
	switch what {
	case "report":
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		summary, err := eval.Run(eval.Deps{DB: db, Cfg: cfg, RepoRoot: "."})
		if err != nil {
			return err
		}
		fmt.Print(summary)
		return nil
	case "fit":
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		deps := eval.Deps{DB: db, Cfg: cfg, RepoRoot: "."}
		var summary string
		switch sub {
		case "weights":
			summary, err = eval.FitWeights(deps)
		case "alphas":
			summary, err = eval.FitAlphas(deps)
		case "completion":
			summary, err = eval.FitCompletion(deps)
		default:
			return usageErr("eval fit " + sub)
		}
		if err != nil {
			return err
		}
		fmt.Print(summary)
		return nil
	case "promote":
		summary, err := eval.Promote(sub, eval.Deps{Cfg: cfg, RepoRoot: ".", CfgPath: cfgPath})
		if err != nil {
			return err
		}
		fmt.Print(summary)
		return nil
	}
	return usageErr("eval " + what)
}

// runDBQuery prints the rows of one read-only sql statement against the live
// db, the make-first verification path for ingest results.
func runDBQuery(q string, cfg *config.Config) error {
	if q == "" {
		return usageErr("db-query")
	}
	db, err := sql.Open("duckdb", cfg.Paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Query(q)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	fmt.Println(strings.Join(cols, "\t"))
	vals := make([]any, len(cols))
	for i := range vals {
		vals[i] = new(sql.NullString)
	}
	for rows.Next() {
		if err := rows.Scan(vals...); err != nil {
			return err
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			cells[i] = v.(*sql.NullString).String
		}
		fmt.Println(strings.Join(cells, "\t"))
	}
	return rows.Err()
}

func arg(i int) string {
	if i < len(os.Args) {
		return os.Args[i]
	}
	return ""
}

func usageErr(what string) error {
	usage()
	return fmt.Errorf("unknown subcommand %q", what)
}

func notImplemented(what string) error {
	return fmt.Errorf("%s: not implemented yet", what)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: engine probe stratz|opendota|builds|matches|scope|window | fetch stratz [-refresh]|opendota|builds [-refresh]|matches|positions | ingest | mine | sync builds|order | db-query <sql> | emit | eval report|fit weights|alphas|completion|promote weights|alphas|completion | fixtures | verify | serve")
}
