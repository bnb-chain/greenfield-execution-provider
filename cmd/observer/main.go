package main

import (
	"flag"
	"fmt"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/bnb-chain/greenfield-execution-provider/client"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/observer"
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
	fmt.Print("usage: ./observer --config-path config_file_path\n")
}

func main() {
	initFlags()

	var config *util.ObserverConfig

	configFilePath := viper.GetString(flagConfigPath)
	if configFilePath == "" {
		printUsage()
		configFilePath = "config_observer.json"
		fmt.Println("Use default config...")
		//return
	}
	config = util.ParseObserverConfigFromFile(configFilePath)
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

	greenfieldClient := client.NewGreenFieldClient(&config.GreenfieldConfig)

	observer := observer.NewObserver(db, config, greenfieldClient)

	observer.Start()

	select {}
}
