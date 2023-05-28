package common

import "time"

const (
	ObserverMaxBlockNumber = 10000
	ObserverPruneInterval  = 10 * time.Second
	ObserverAlertInterval  = 5 * time.Second
	ObserverFetchInterval  = 2 * time.Second

	SenderSendInterval = 1 * time.Second

	DefaultConfirmNum int64 = 15
)

const (
	ExecutionTaskEvent   = "greenfield.storage.EventExecutionTask"
	ExecutionResultEvent = "greenfield.storage.EventExecutionResult"
)

const (
	DBDialectMysql   = "mysql"
	DBDialectSqlite3 = "sqlite3"
)

type BlockAndEventLogs struct {
	Height          int64
	BlockHash       string
	ParentBlockHash string
	BlockTime       int64
	Events          []interface{}
}
