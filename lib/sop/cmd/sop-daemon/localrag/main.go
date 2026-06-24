package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/agent"
	"github.com/sharedcode/sop/ai/database"
)

func main() {
	configPath := flag.String("config", "", "Path to a local agent config JSON file")
	storagePath := flag.String("storage", "", "Override storage root for the local RAG binary")
	stubMode := flag.Bool("stub", false, "Enable stub mode for local tools")
	flag.Parse()

	if *configPath == "" {
		fmt.Println("Usage: go run ./cmd/sop-daemon/localrag -config <path-to-config.json> [--storage <path>] [--stub]")
		os.Exit(1)
	}

	cfg, err := agent.LoadConfigFromFile(*configPath)
	if err != nil {
		panic(fmt.Errorf("failed to load config: %w", err))
	}

	if err := applyOverrides(cfg, *storagePath, *stubMode); err != nil {
		panic(err)
	}

	fmt.Printf("Initializing local RAG runtime for %s...\n", cfg.Name)
	registry := make(map[string]ai.Agent[map[string]any])

	initAgent := func(agentCfg agent.Config) (ai.Agent[map[string]any], error) {
		if agentCfg.StoragePath != "" {
			if absPath, err := filepath.Abs(agentCfg.StoragePath); err == nil {
				agentCfg.StoragePath = absPath
			}
		}
		return agent.NewFromConfig(context.Background(), agentCfg, agent.Dependencies{AgentRegistry: registry})
	}

	for _, pCfg := range cfg.Policies {
		if pCfg.ID != "" {
			registry[pCfg.ID] = agent.NewPolicyAgent(pCfg.ID, nil, nil)
		}
	}

	for _, localAgentCfg := range cfg.Agents {
		if localAgentCfg.ID == "" {
			continue
		}
		if _, exists := registry[localAgentCfg.ID]; exists {
			continue
		}
		svc, err := initAgent(localAgentCfg)
		if err != nil {
			panic(fmt.Errorf("failed to initialize local agent %s: %w", localAgentCfg.ID, err))
		}
		registry[localAgentCfg.ID] = svc
	}

	deps := agent.Dependencies{AgentRegistry: registry}
	if cfg.StoragePath != "" {
		dbPath := filepath.Join(cfg.StoragePath, "db")
		dbType := sop.Standalone
		if cfg.DBType == "clustered" {
			dbType = sop.Clustered
		}
		dbOpts := sop.DatabaseOptions{StoresFolders: []string{dbPath}, Type: dbType}
		sysDB := database.NewDatabase(dbOpts)
		deps.SystemDB = sysDB
		deps.Databases = map[string]sop.DatabaseOptions{"default": dbOpts}
	}

	svc, err := agent.NewFromConfig(context.Background(), *cfg, deps)
	if err != nil {
		panic(fmt.Errorf("failed to initialize service: %w", err))
	}

	assistantName := cfg.AssistantName
	if assistantName == "" {
		assistantName = "Local RAG"
	}
	fmt.Printf("\n%s ready. Type 'exit' to quit.\n", assistantName)
	fmt.Println("Ask questions locally; the runtime uses your local storage path and any configured local embedder/generator.")
	if err := agent.RunLoop(context.Background(), svc, os.Stdin, os.Stdout, cfg.UserPrompt, assistantName, ai.NewConfigMap()); err != nil {
		fmt.Printf("Error during session: %v\n", err)
	}
}

func applyOverrides(cfg *agent.Config, storagePath string, stubMode bool) error {
	if cfg == nil {
		return nil
	}
	if stubMode {
		cfg.StubMode = true
		for i := range cfg.Agents {
			cfg.Agents[i].StubMode = true
		}
	}
	if strings.TrimSpace(storagePath) != "" {
		absPath, err := filepath.Abs(storagePath)
		if err != nil {
			return fmt.Errorf("failed to resolve storage path: %w", err)
		}
		cfg.StoragePath = absPath
		for i := range cfg.Agents {
			if strings.TrimSpace(cfg.Agents[i].StoragePath) == "" {
				cfg.Agents[i].StoragePath = absPath
			} else {
				absAgentPath, err := filepath.Abs(cfg.Agents[i].StoragePath)
				if err != nil {
					return fmt.Errorf("failed to resolve agent storage path: %w", err)
				}
				cfg.Agents[i].StoragePath = absAgentPath
			}
		}
	} else if cfg.StoragePath != "" {
		absPath, err := filepath.Abs(cfg.StoragePath)
		if err != nil {
			return fmt.Errorf("failed to resolve storage path: %w", err)
		}
		cfg.StoragePath = absPath
		for i := range cfg.Agents {
			if strings.TrimSpace(cfg.Agents[i].StoragePath) == "" {
				cfg.Agents[i].StoragePath = absPath
			} else {
				absAgentPath, err := filepath.Abs(cfg.Agents[i].StoragePath)
				if err != nil {
					return fmt.Errorf("failed to resolve agent storage path: %w", err)
				}
				cfg.Agents[i].StoragePath = absAgentPath
			}
		}
	}
	return nil
}
