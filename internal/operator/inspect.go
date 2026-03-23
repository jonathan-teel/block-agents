package operator

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"aichain/internal/protocol"
)

func runInspectChain(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect chain", stderr)
	opts := bindInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("inspect chain does not accept positional arguments")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	info, err := client.ChainInfo(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, info)
	}
	renderChainInfo(stdout, info)
	return nil
}

func runInspectBlock(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect block", stderr)
	opts := bindInspectFlags(fs)
	heightArg := "head"
	fs.StringVar(&heightArg, "height", "head", "block height or 'head'")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("inspect block accepts at most one positional height")
	}
	if fs.NArg() == 1 {
		heightArg = fs.Arg(0)
	}

	client := NewClient(opts.RPCURL, opts.Timeout)

	var (
		block protocol.Block
		err   error
	)

	switch strings.TrimSpace(strings.ToLower(heightArg)) {
	case "", "head":
		block, err = client.HeadBlock(ctx)
	default:
		height, parseErr := strconv.ParseInt(heightArg, 10, 64)
		if parseErr != nil || height < 0 {
			return fmt.Errorf("invalid block height %q", heightArg)
		}
		block, err = client.BlockByHeight(ctx, height)
	}
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, block)
	}
	renderBlock(stdout, block)
	return nil
}

func runInspectTransaction(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect tx", stderr)
	opts := bindInspectFlags(fs)
	hash := ""
	fs.StringVar(&hash, "hash", "", "transaction hash")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("inspect tx accepts at most one positional hash")
	}
	if fs.NArg() == 1 {
		hash = fs.Arg(0)
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("transaction hash is required")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	status, err := client.Transaction(ctx, hash)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, status)
	}
	renderTransactionStatus(stdout, status)
	return nil
}

func runInspectTask(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect task", stderr)
	opts := bindInspectFlags(fs)
	id := ""
	fs.StringVar(&id, "id", "", "task identifier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("inspect task accepts at most one positional task identifier")
	}
	if fs.NArg() == 1 {
		id = fs.Arg(0)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("task identifier is required")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	task, err := client.Task(ctx, id)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, task)
	}
	renderTaskDetails(stdout, task)
	return nil
}

func runInspectAgent(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect agent", stderr)
	opts := bindInspectFlags(fs)
	address := ""
	fs.StringVar(&address, "address", "", "agent address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("inspect agent accepts at most one positional address")
	}
	if fs.NArg() == 1 {
		address = fs.Arg(0)
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("agent address is required")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	agent, err := client.Agent(ctx, address)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, agent)
	}
	renderAgent(stdout, agent)
	return nil
}

func runInspectPeers(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect peers", stderr)
	opts := bindInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("inspect peers does not accept positional arguments")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	peers, err := client.Peers(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, peers)
	}
	renderPeers(stdout, peers)
	return nil
}

func runInspectValidators(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect validators", stderr)
	opts := bindInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("inspect validators does not accept positional arguments")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	validators, err := client.Validators(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, validators)
	}
	renderValidators(stdout, validators)
	return nil
}

func runInspectOpenTasks(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect open-tasks", stderr)
	opts := bindInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("inspect open-tasks does not accept positional arguments")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	tasks, err := client.OpenTasks(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, tasks)
	}
	renderOpenTasks(stdout, tasks)
	return nil
}

func runInspectSync(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := newFlagSet("inspect sync", stderr)
	opts := bindInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("inspect sync does not accept positional arguments")
	}

	client := NewClient(opts.RPCURL, opts.Timeout)
	status, err := client.SyncStatus(ctx)
	if err != nil {
		return err
	}

	if opts.JSON {
		return writeJSON(stdout, status)
	}
	renderSyncStatus(stdout, status)
	return nil
}
