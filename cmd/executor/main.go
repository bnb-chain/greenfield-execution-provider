package main

import (
	"flag"
	"fmt"

	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield-go-sdk/types"

	"github.com/bnb-chain/greenfield-execution-provider/executor"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/bnb-chain/greenfield-execution-provider/model"

	"github.com/bnb-chain/greenfield-execution-provider/util"
)

const (
	flagConfigPath = "config-path"
)

func initFlags() {
	flag.String(flagConfigPath, "", "config path")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		panic(fmt.Sprintf("bind flags error, err=%s", err))
	}
}

func printUsage() {
	fmt.Print("usage: ./executor --config-path config_file_path\n")
}

func main() {
	initFlags()

	var config *util.ExecutorConfig

	configFilePath := viper.GetString(flagConfigPath)
	if configFilePath == "" {
		printUsage()
		fmt.Println("Use default executor config")
		configFilePath = "config_executor.json"
		// return
	}
	config = util.ParseExecutorConfigFromFile(configFilePath)
	config.Validate()

	// init logger
	util.InitLogger(*config.LogConfig)
	util.InitAlert(config.AlertConfig)

	db, err := gorm.Open(config.DBConfig.Dialect, config.DBConfig.DBPath)
	if err != nil {
		panic(fmt.Sprintf("open db error, err=%s", err.Error()))
	}
	defer db.Close()
	model.InitTables(db)

	account, err := types.NewAccountFromMnemonic("executor", config.GreenfieldConfig.PrivateKey)
	if err != nil {
		panic(err)
	}

	sdkClient, err := sdkclient.New(config.GreenfieldConfig.ChainIdString, config.GreenfieldConfig.RPCAddr, sdkclient.Option{DefaultAccount: account})
	if err != nil {
		panic(err)
	}

	executor := executor.NewExecutor(db, sdkClient)
	executor.Start()

	select {}
}
