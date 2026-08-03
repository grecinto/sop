package sopbackend

import (
	"github.com/sharedcode/sop/ai"
)

type HostKind string

const (
	HostKindUnknown   HostKind = "unknown"
	HostKindHQ        HostKind = "hq"
	HostKindDaemon    HostKind = "daemon"
	HostKindCorporate HostKind = "corporate"
)

type AuthMode string

const (
	AuthModeUnknown       AuthMode = "unknown"
	AuthModeSession       AuthMode = "session"
	AuthModeJWTBootstrap  AuthMode = "jwt_bootstrap"
	AuthModeEnterpriseSSO AuthMode = "enterprise_sso"
)

type ProvisioningMode string

const (
	ProvisioningModeUnknown  ProvisioningMode = "unknown"
	ProvisioningModeSimple   ProvisioningMode = "simple"
	ProvisioningModeAdvanced ProvisioningMode = "advanced"
)

type HostProfile struct {
	Kind                    HostKind           `json:"kind"`
	AuthMode                AuthMode           `json:"auth_mode"`
	ProvisioningModes       []ProvisioningMode `json:"provisioning_modes,omitempty"`
	DefaultProvisioningMode ProvisioningMode   `json:"default_provisioning_mode,omitempty"`
}

// ErasureConfigEntry defines a single EC zone configuration
type ErasureConfigEntry struct {
	Key          string   `json:"key"`
	DataChunks   int      `json:"data_chunks"`
	ParityChunks int      `json:"parity_chunks"`
	BasePaths    []string `json:"base_paths"`
}

// DatabaseConfig holds configuration for a single SOP database
type DatabaseConfig struct {
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	StoresFolders []string `json:"stores_folders,omitempty"`
	Mode          string   `json:"mode"`  // "standalone" or "clustered"
	RedisURL      string   `json:"redis"` // Optional, for clustered
	IsSystem      bool     `json:"is_system,omitempty"`
	Warning       string   `json:"warning,omitempty"` // New field for UI alerting

	// ErasureConfigs stores a list of EC configurations.
	ErasureConfigs []ErasureConfigEntry `json:"erasure_configs,omitempty"`

	// EnableObfuscation specifies if this database should be obfuscated when accessed by AI tools.
	// This allows per-database granular control.
	EnableObfuscation bool `json:"enable_obfuscation,omitempty"`
}

// Config holds the server configuration
type Config struct {
	Port                   int              `json:"port"`
	Databases              []DatabaseConfig `json:"databases"`
	PageSize               int              `json:"pageSize"`
	SystemDB               *DatabaseConfig  `json:"system_db,omitempty"`
	RootPassword           SecuredString    `json:"root_password,omitempty"`
	ProductionMode         bool             `json:"production_mode,omitempty"`
	SessionTokenTTLMinutes int              `json:"session_token_ttl_minutes,omitempty"`
	SessionSecret          SecuredString    `json:"session_secret,omitempty"`
	AuthProviderName       string           `json:"auth_provider,omitempty"`
	UsersStore             UserStore        `json:"-"`
	persistentUsersStore   UserStore        `json:"-"`

	// ObfuscationMode defines the global obfuscation policy (disabled, per_database, all_databases).
	// This overrides any setting in the agent's own configuration.
	ObfuscationMode string `json:"obfuscation_mode,omitempty"`

	// Legacy/CLI fields - Ignored in JSON to keep config clean
	HostProfile                      HostProfile `json:"-"`
	HostKind                         HostKind    `json:"-"`
	DatabasePath                     string      `json:"-"`
	Mode                             string      `json:"-"`
	ConfigFile                       string      `json:"-"`
	RedisURL                         string      `json:"-"`
	DisableAuth                      bool        `json:"-"` // Disable authentication (for local runtime)
	DisableSOPKBPreload              bool        `json:"disable_sop_kb_preload,omitempty"`
	VectorizationRecoveryStoragePath string      `json:"vectorization_recovery_storage_path,omitempty"`
}

// Package-level global variables used by handler functions
var config Config
var modelCatalog = DefaultModelCatalog()
var loadedAgents = make(map[string]ai.Agent[map[string]any])
var activeSessions = NewSessionManager(100)

const SystemDBName = "system"

// FirstNonEmpty returns the first non-empty string from the provided values
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
