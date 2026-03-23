package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"aichain/internal/config"
	"aichain/internal/protocol"
	"aichain/internal/storage/postgres"
)

type InitChainReport struct {
	Status      string            `json:"status"`
	Chain       protocol.ChainInfo `json:"chain"`
	DatabaseURL string            `json:"database_url"`
	GenesisFile string            `json:"genesis_file,omitempty"`
	Genesis     protocol.Genesis  `json:"genesis"`
}

func runInitChain(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("init chain", stderr)
	jsonOutput := fs.Bool("json", false, "emit JSON instead of formatted terminal output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("init chain does not accept positional arguments")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store, err := postgres.New(cfg)
	if err != nil {
		return err
	}
	defer store.Close()

	info, err := store.GetChainInfo(ctx)
	if err != nil {
		return err
	}
	info.BlockIntervalSeconds = int64(cfg.BlockInterval.Seconds())
	info.MaxTransactionsPerBlock = cfg.MaxTransactionsPerBlock
	info.FaucetEnabled = cfg.EnableFaucet

	report := InitChainReport{
		Status:      "ready",
		Chain:       info,
		DatabaseURL: sanitizeDatabaseURL(cfg.DatabaseURL),
		GenesisFile: strings.TrimSpace(os.Getenv("GENESIS_FILE")),
		Genesis:     cfg.Genesis,
	}

	if *jsonOutput {
		return writeJSON(stdout, report)
	}
	renderInitChainReport(stdout, report)
	return nil
}

func sanitizeDatabaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}

	if parsed.User != nil {
		username := parsed.User.Username()
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(username, "xxxxx")
		}
	}
	return parsed.String()
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
