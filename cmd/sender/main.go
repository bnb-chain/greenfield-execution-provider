package main

import (
	"flag"
	"fmt"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/sender"
	"github.com/bnb-chain/greenfield-execution-provider/util"
	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	sdktypes "github.com/bnb-chain/greenfield-go-sdk/types"
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
	fmt.Print("usage: ./sender --config-path config_file_path\n")
}

func main() {
	initFlags()

	var config *util.SenderConfig

	configFilePath := viper.GetString(flagConfigPath)
	if configFilePath == "" {
		printUsage()
		return
	}
	config = util.ParseSenderConfigFromFile(configFilePath)

	// init logger
	util.InitLogger(*config.LogConfig)
	util.InitAlert(config.AlertConfig)

	db, err := gorm.Open(config.DBConfig.Dialect, config.DBConfig.DBPath)
	if err != nil {
		panic(fmt.Sprintf("open db error, err=%s", err.Error()))
	}
	defer db.Close()
	model.InitTables(db)

	account, err := sdktypes.NewAccountFromMnemonic("sender", config.GreenfieldConfig.PrivateKey)
	if err != nil {
		panic(err)
	}

	sdkClient, err := sdkclient.New(config.GreenfieldConfig.ChainIdString, config.GreenfieldConfig.RPCAddr, sdkclient.Option{DefaultAccount: account})
	if err != nil {
		panic(err)
	}

	snder := sender.NewSender(db, sdkClient)
	snder.Start()

	select {}
}
