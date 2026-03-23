package operator

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const defaultInspectTimeout = 5 * time.Second

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printRootHelp(stdout)
		return nil
	}

	switch normalizeCommand(args[0]) {
	case "help", "-h", "--help":
		printRootHelp(stdout)
		return nil
	case "init":
		return runInit(ctx, args[1:], stdout, stderr)
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr)
	default:
		printRootHelp(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printInitHelp(stdout)
		return nil
	}

	switch normalizeCommand(args[0]) {
	case "help", "-h", "--help":
		printInitHelp(stdout)
		return nil
	case "chain":
		return runInitChain(ctx, args[1:], stdout, stderr)
	default:
		printInitHelp(stderr)
		return fmt.Errorf("unknown init command %q", args[0])
	}
}

func runInspect(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printInspectHelp(stdout)
		return nil
	}

	switch normalizeCommand(args[0]) {
	case "help", "-h", "--help":
		printInspectHelp(stdout)
		return nil
	case "chain":
		return runInspectChain(ctx, args[1:], stdout, stderr)
	case "block":
		return runInspectBlock(ctx, args[1:], stdout, stderr)
	case "tx":
		return runInspectTransaction(ctx, args[1:], stdout, stderr)
	case "task":
		return runInspectTask(ctx, args[1:], stdout, stderr)
	case "agent":
		return runInspectAgent(ctx, args[1:], stdout, stderr)
	case "peers":
		return runInspectPeers(ctx, args[1:], stdout, stderr)
	case "validators":
		return runInspectValidators(ctx, args[1:], stdout, stderr)
	case "open-tasks":
		return runInspectOpenTasks(ctx, args[1:], stdout, stderr)
	case "sync":
		return runInspectSync(ctx, args[1:], stdout, stderr)
	default:
		printInspectHelp(stderr)
		return fmt.Errorf("unknown inspect command %q", args[0])
	}
}

func normalizeCommand(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

type inspectOptions struct {
	RPCURL  string
	Timeout time.Duration
	JSON    bool
}

func bindInspectFlags(fs *flag.FlagSet) *inspectOptions {
	opts := &inspectOptions{}
	fs.StringVar(&opts.RPCURL, "rpc", defaultRPCURL(), "BlockAgents RPC base URL")
	fs.DurationVar(&opts.Timeout, "timeout", defaultInspectTimeout, "HTTP request timeout")
	fs.BoolVar(&opts.JSON, "json", false, "emit JSON instead of formatted terminal output")
	return opts
}

func defaultRPCURL() string {
	for _, key := range []string{"BLOCKAGENTS_RPC_URL", "P2P_LISTEN_ADDR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	port = strings.TrimPrefix(port, ":")
	return "http://127.0.0.1:" + port
}

func printRootHelp(w io.Writer) {
	fmt.Fprintln(w, "BlockAgents operator CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  blockagentsctl init chain [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect chain [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect block [HEIGHT|head] [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect tx <HASH> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect task <TASK_ID> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect agent <ADDRESS> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect peers [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect validators [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect open-tasks [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect sync [--rpc URL] [--json]")
}

func printInitHelp(w io.Writer) {
	fmt.Fprintln(w, "Initialize BlockAgents chain state")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  blockagentsctl init chain [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The command loads runtime configuration from the current environment,")
	fmt.Fprintln(w, "applies schema migrations, ensures genesis state exists, and prints")
	fmt.Fprintln(w, "an operator-friendly summary of the resulting chain.")
}

func printInspectHelp(w io.Writer) {
	fmt.Fprintln(w, "Inspect a running BlockAgents node")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  blockagentsctl inspect chain [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect block [HEIGHT|head] [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect tx <HASH> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect task <TASK_ID> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect agent <ADDRESS> [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect peers [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect validators [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect open-tasks [--rpc URL] [--json]")
	fmt.Fprintln(w, "  blockagentsctl inspect sync [--rpc URL] [--json]")
}
