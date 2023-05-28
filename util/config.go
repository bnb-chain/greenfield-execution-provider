package util

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bnb-chain/greenfield-execution-provider/common"
)

type ObserverConfig struct {
	DBConfig         *DBConfig        `json:"db_config"`
	GreenfieldConfig GreenfieldConfig `json:"greenfield_config"`
	LogConfig        *LogConfig       `json:"log_config"`
	AlertConfig      *AlertConfig     `json:"alert_config"`
}

func (cfg *ObserverConfig) Validate() {
	cfg.DBConfig.Validate()
	cfg.LogConfig.Validate()
	cfg.AlertConfig.Validate()
}

type SenderConfig struct {
	DBConfig         *DBConfig        `json:"db_config"`
	GreenfieldConfig GreenfieldConfig `json:"greenfield_config"`
	LogConfig        *LogConfig       `json:"log_config"`
	AlertConfig      *AlertConfig     `json:"alert_config"`
}

type AlertConfig struct {
	Moniker string `json:"moniker"`

	SlackApp string `json:"slack_app"`

	BlockUpdateTimeout int64 `json:"block_update_timeout"`
}

func (cfg *AlertConfig) Validate() {
	if cfg.Moniker == "" {
		panic("moniker should not be empty")
	}
}

type DBConfig struct {
	Dialect string `json:"dialect"`
	DBPath  string `json:"db_path"`
}

func (cfg *DBConfig) Validate() {
	if cfg.Dialect != common.DBDialectMysql && cfg.Dialect != common.DBDialectSqlite3 {
		panic(fmt.Sprintf("only %s and %s supported", common.DBDialectMysql, common.DBDialectSqlite3))
	}
	if cfg.DBPath == "" {
		panic("db path should not be empty")
	}
}

type LogConfig struct {
	Level                        string `json:"level"`
	Filename                     string `json:"filename"`
	MaxFileSizeInMB              int    `json:"max_file_size_in_mb"`
	MaxBackupsOfLogFiles         int    `json:"max_backups_of_log_files"`
	MaxAgeToRetainLogFilesInDays int    `json:"max_age_to_retain_log_files_in_days"`
	UseConsoleLogger             bool   `json:"use_console_logger"`
	UseFileLogger                bool   `json:"use_file_logger"`
	Compress                     bool   `json:"compress"`
}

func (cfg *LogConfig) Validate() {
	if cfg.UseFileLogger {
		if cfg.Filename == "" {
			panic("filename should not be empty if use file logger")
		}
		if cfg.MaxFileSizeInMB <= 0 {
			panic("max_file_size_in_mb should be larger than 0 if use file logger")
		}
		if cfg.MaxBackupsOfLogFiles <= 0 {
			panic("max_backups_off_log_files should be larger than 0 if use file logger")
		}
	}
}

type GreenfieldConfig struct {
	RPCAddr       string `json:"rpc_addr"`
	PrivateKey    string `json:"private_key"`
	ChainId       uint64 `json:"chain_id"`
	StartHeight   int64  `json:"start_height"`
	GasLimit      uint64 `json:"gas_limit"`
	FeeAmount     uint64 `json:"fee_amount"`
	ChainIdString string `json:"chain_id_string"`
}

// ParseObserverConfigFromFile returns the config from json file
func ParseObserverConfigFromFile(filePath string) *ObserverConfig {
	bz, err := os.ReadFile(filePath)
	if err != nil {
		panic(err)
	}

	var config ObserverConfig
	if err := json.Unmarshal(bz, &config); err != nil {
		panic(err)
	}
	return &config
}

// ParseSenderConfigFromFile returns the config from json file
func ParseSenderConfigFromFile(filePath string) *SenderConfig {
	bz, err := os.ReadFile(filePath)
	if err != nil {
		panic(err)
	}

	var config SenderConfig
	if err := json.Unmarshal(bz, &config); err != nil {
		panic(err)
	}
	return &config
}
