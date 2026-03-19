package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"aichain/internal/protocol"
	"aichain/internal/txauth"
)

type Config struct {
	Port                    string
	P2PListenAddr           string
	SeedPeers               []string
	DatabaseURL             string
	ChainID                 string
	NodeID                  string
	ValidatorAddress        string
	ValidatorPrivateKey     string
	ConsensusRoundTimeout   time.Duration
	SyncLookaheadBlocks     int
	RoleSelectionPolicy     string
	MinerVotePolicy         string
	ReorgPolicy             string
	AllowEarlyDebateAdvance bool
	MinEvaluationsPerProposal int
	MinVotesPerRound        int
	ValidatorSlashFraction  float64
	ValidatorSlashReputationPenalty float64
	BlockInterval           time.Duration
	MaxTransactionsPerBlock int
	MaxEffectiveWeight      float64
	CreateEmptyBlocks       bool
	EnableFaucet            bool
	FaucetGrantAmount       float64
	DefaultAgentReputation  float64
	Genesis                 protocol.Genesis
}

func Load() (Config, error) {
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "blockagentsd-1"
	}

	cfg := Config{
		Port:                    getEnv("PORT", "8080"),
		P2PListenAddr:           getEnv("P2P_LISTEN_ADDR", "http://127.0.0.1:"+getEnv("PORT", "8080")),
		SeedPeers:               splitCSVEnv("SEED_PEERS"),
		DatabaseURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ChainID:                 getEnv("CHAIN_ID", "blockagents-devnet-1"),
		NodeID:                  getEnv("NODE_ID", hostname),
		ValidatorAddress:        getEnv("VALIDATOR_ADDRESS", "validator-1"),
		ValidatorPrivateKey:     txauth.NormalizePublicKey(os.Getenv("VALIDATOR_PRIVATE_KEY")),
		ConsensusRoundTimeout:   time.Duration(getIntEnv("CONSENSUS_ROUND_TIMEOUT_SECONDS", 10)) * time.Second,
		SyncLookaheadBlocks:     getIntEnv("SYNC_LOOKAHEAD_BLOCKS", 6),
		RoleSelectionPolicy:     getEnv("ROLE_SELECTION_POLICY", "balance_reputation"),
		MinerVotePolicy:         getEnv("MINER_VOTE_POLICY", "reputation_weighted"),
		ReorgPolicy:             getEnv("REORG_POLICY", "best_certified"),
		AllowEarlyDebateAdvance: getBoolEnv("ALLOW_EARLY_DEBATE_ADVANCE", true),
		MinEvaluationsPerProposal: getIntEnv("MIN_EVALUATIONS_PER_PROPOSAL", 1),
		MinVotesPerRound:        getIntEnv("MIN_VOTES_PER_ROUND", 1),
		ValidatorSlashFraction:  getFloatEnv("VALIDATOR_SLASH_FRACTION", 0.1),
		ValidatorSlashReputationPenalty: getFloatEnv("VALIDATOR_SLASH_REPUTATION_PENALTY", 0.2),
		BlockInterval:           time.Duration(getIntEnv("BLOCK_INTERVAL_SECONDS", 5)) * time.Second,
		MaxTransactionsPerBlock: getIntEnv("MAX_TRANSACTIONS_PER_BLOCK", 250),
		MaxEffectiveWeight:      getFloatEnv("MAX_EFFECTIVE_WEIGHT", 100),
		CreateEmptyBlocks:       getBoolEnv("CREATE_EMPTY_BLOCKS", true),
		EnableFaucet:            getBoolEnv("ENABLE_FAUCET", true),
		FaucetGrantAmount:       getFloatEnv("FAUCET_GRANT_AMOUNT", 1000),
		DefaultAgentReputation:  getFloatEnv("DEFAULT_AGENT_REPUTATION", 0.5),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.BlockInterval <= 0 {
		return Config{}, fmt.Errorf("BLOCK_INTERVAL_SECONDS must be > 0")
	}
	if cfg.ConsensusRoundTimeout <= 0 {
		return Config{}, fmt.Errorf("CONSENSUS_ROUND_TIMEOUT_SECONDS must be > 0")
	}
	if cfg.SyncLookaheadBlocks <= 0 {
		return Config{}, fmt.Errorf("SYNC_LOOKAHEAD_BLOCKS must be > 0")
	}
	if cfg.MinEvaluationsPerProposal <= 0 {
		return Config{}, fmt.Errorf("MIN_EVALUATIONS_PER_PROPOSAL must be > 0")
	}
	if cfg.MinVotesPerRound <= 0 {
		return Config{}, fmt.Errorf("MIN_VOTES_PER_ROUND must be > 0")
	}
	if cfg.MaxTransactionsPerBlock <= 0 {
		return Config{}, fmt.Errorf("MAX_TRANSACTIONS_PER_BLOCK must be > 0")
	}
	if cfg.MaxEffectiveWeight <= 0 {
		return Config{}, fmt.Errorf("MAX_EFFECTIVE_WEIGHT must be > 0")
	}
	if cfg.FaucetGrantAmount <= 0 {
		return Config{}, fmt.Errorf("FAUCET_GRANT_AMOUNT must be > 0")
	}
	if cfg.DefaultAgentReputation < 0 || cfg.DefaultAgentReputation > 1 {
		return Config{}, fmt.Errorf("DEFAULT_AGENT_REPUTATION must be within [0,1]")
	}
	if cfg.ValidatorSlashFraction < 0 || cfg.ValidatorSlashFraction > 1 {
		return Config{}, fmt.Errorf("VALIDATOR_SLASH_FRACTION must be within [0,1]")
	}
	if cfg.ValidatorSlashReputationPenalty < 0 || cfg.ValidatorSlashReputationPenalty > 1 {
		return Config{}, fmt.Errorf("VALIDATOR_SLASH_REPUTATION_PENALTY must be within [0,1]")
	}
	if cfg.RoleSelectionPolicy != "balance_reputation" && cfg.RoleSelectionPolicy != "reputation_balance" && cfg.RoleSelectionPolicy != "round_robin_hash" {
		return Config{}, fmt.Errorf("ROLE_SELECTION_POLICY must be one of balance_reputation, reputation_balance, round_robin_hash")
	}
	if cfg.MinerVotePolicy != "reputation_weighted" && cfg.MinerVotePolicy != "one_agent_one_vote" {
		return Config{}, fmt.Errorf("MINER_VOTE_POLICY must be one of reputation_weighted, one_agent_one_vote")
	}
	if cfg.ReorgPolicy != "forward_only" && cfg.ReorgPolicy != "best_certified" && cfg.ReorgPolicy != "manual" {
		return Config{}, fmt.Errorf("REORG_POLICY must be one of forward_only, best_certified, manual")
	}
	if cfg.ValidatorPrivateKey != "" {
		decoded, err := hex.DecodeString(cfg.ValidatorPrivateKey)
		if err != nil || len(decoded) != 64 {
			return Config{}, fmt.Errorf("VALIDATOR_PRIVATE_KEY must be a 64-byte hex ed25519 private key")
		}
	}

	genesis, err := loadGenesis(cfg.ChainID)
	if err != nil {
		return Config{}, err
	}
	cfg.Genesis = genesis
	cfg.ChainID = genesis.ChainID

	return cfg, nil
}

func loadGenesis(chainID string) (protocol.Genesis, error) {
	path := strings.TrimSpace(os.Getenv("GENESIS_FILE"))
	if path == "" {
		return defaultGenesis(chainID), nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return protocol.Genesis{}, fmt.Errorf("read genesis file: %w", err)
	}

	var genesis protocol.Genesis
	if err := json.Unmarshal(raw, &genesis); err != nil {
		return protocol.Genesis{}, fmt.Errorf("decode genesis file: %w", err)
	}

	normalizeGenesis(&genesis, chainID)
	if err := validateGenesis(genesis); err != nil {
		return protocol.Genesis{}, err
	}

	return genesis, nil
}

func defaultGenesis(chainID string) protocol.Genesis {
	return protocol.Genesis{
		ChainID:       chainID,
		GenesisTime:   time.Now().UTC().Truncate(time.Second),
		FaucetAddress: "faucet",
		Accounts: []protocol.GenesisAccount{
			{Address: "faucet", Balance: 1_000_000, Reputation: 1},
			{Address: "alice", Balance: 10_000, Reputation: 0.65},
			{Address: "bob", Balance: 10_000, Reputation: 0.6},
			{Address: "carol", Balance: 10_000, Reputation: 0.55},
		},
		Validators: []protocol.GenesisValidator{
			{Address: "validator-1", PublicKey: "0000000000000000000000000000000000000000000000000000000000000000", Power: 1},
		},
	}
}

func normalizeGenesis(genesis *protocol.Genesis, fallbackChainID string) {
	genesis.ChainID = strings.TrimSpace(genesis.ChainID)
	if genesis.ChainID == "" {
		genesis.ChainID = fallbackChainID
	}

	genesis.FaucetAddress = strings.TrimSpace(genesis.FaucetAddress)
	if genesis.FaucetAddress == "" {
		genesis.FaucetAddress = "faucet"
	}

	if genesis.GenesisTime.IsZero() {
		genesis.GenesisTime = time.Now().UTC().Truncate(time.Second)
	}
	genesis.GenesisTime = genesis.GenesisTime.UTC().Truncate(time.Second)

	for index := range genesis.Accounts {
		genesis.Accounts[index].Address = strings.TrimSpace(genesis.Accounts[index].Address)
		genesis.Accounts[index].PublicKey = txauth.NormalizePublicKey(genesis.Accounts[index].PublicKey)
	}
	for index := range genesis.Validators {
		genesis.Validators[index].Address = strings.TrimSpace(genesis.Validators[index].Address)
		genesis.Validators[index].PublicKey = txauth.NormalizePublicKey(genesis.Validators[index].PublicKey)
	}
}

func validateGenesis(genesis protocol.Genesis) error {
	if strings.TrimSpace(genesis.ChainID) == "" {
		return fmt.Errorf("genesis chain_id is required")
	}
	if len(genesis.Accounts) == 0 {
		return fmt.Errorf("genesis must define at least one account")
	}

	addresses := make(map[string]struct{}, len(genesis.Accounts))
	for _, account := range genesis.Accounts {
		if account.Address == "" {
			return fmt.Errorf("genesis account address is required")
		}
		if account.Balance < 0 {
			return fmt.Errorf("genesis account %s balance must be >= 0", account.Address)
		}
		if account.PublicKey != "" {
			decoded, err := hex.DecodeString(account.PublicKey)
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("genesis account %s public_key must be a 32-byte hex ed25519 key", account.Address)
			}
		}
		if account.Reputation < 0 || account.Reputation > 1 {
			return fmt.Errorf("genesis account %s reputation must be within [0,1]", account.Address)
		}
		if _, exists := addresses[account.Address]; exists {
			return fmt.Errorf("genesis account %s is duplicated", account.Address)
		}
		addresses[account.Address] = struct{}{}
	}

	if len(genesis.Validators) == 0 {
		return fmt.Errorf("genesis must define at least one validator")
	}
	validatorAddresses := make(map[string]struct{}, len(genesis.Validators))
	for _, validator := range genesis.Validators {
		if validator.Address == "" {
			return fmt.Errorf("genesis validator address is required")
		}
		decoded, err := hex.DecodeString(validator.PublicKey)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("genesis validator %s public_key must be a 32-byte hex ed25519 key", validator.Address)
		}
		if validator.Power <= 0 {
			return fmt.Errorf("genesis validator %s power must be > 0", validator.Address)
		}
		if _, exists := validatorAddresses[validator.Address]; exists {
			return fmt.Errorf("genesis validator %s is duplicated", validator.Address)
		}
		validatorAddresses[validator.Address] = struct{}{}
	}

	if _, exists := addresses[genesis.FaucetAddress]; !exists {
		return fmt.Errorf("faucet address %s must be present in genesis accounts", genesis.FaucetAddress)
	}

	return nil
}

func getEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

func getFloatEnv(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}

	return value
}

func getBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}

	return value
}

func splitCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
