package model

import (
	"time"

	"github.com/jinzhu/gorm"
)

type BlockLog struct {
	Id         int64
	Chain      string
	BlockHash  string
	ParentHash string
	Height     int64
	BlockTime  int64
	CreateTime int64
}

func (BlockLog) TableName() string {
	return "block_log"
}

func (l *BlockLog) BeforeCreate() (err error) {
	l.CreateTime = time.Now().Unix()
	return nil
}

type EventStatus int

const (
	EventStatusInit      EventStatus = 0
	EventStatusConfirmed EventStatus = 1
)

type EventLog struct {
	Id        int64
	EventName string

	TaskId             int64
	Operator           string
	ExecutableObjectId string
	InputObjectIds     string
	MaxGas             string
	Method             string
	Params             string

	Status       EventStatus
	BlockHash    string
	TxHash       string
	Height       int64
	ConfirmedNum int64
	CreateTime   int64
	UpdateTime   int64
}

func (EventLog) TableName() string {
	return "event_log"
}

func (l *EventLog) BeforeCreate() (err error) {
	l.CreateTime = time.Now().Unix()
	return nil
}

type ExecutionTaskStatus int

const (
	ExecutionTaskStatusStatusInit             ExecutionTaskStatus = 0 // just created by observer
	ExecutionTaskStatusStatusExecuted         ExecutionTaskStatus = 1 // executed by executor
	ExecutionTaskStatusStatusReceiptSubmitted ExecutionTaskStatus = 2 // receipt submitted by sender
)

type ExecutionTask struct {
	Id int64

	InvokeTxHash string
	TaskId       int64

	ExecutionObjectId int64
	ExecutionUri      string

	InputFiles   string // split by ","
	MaxGas       int64
	InvokeMethod string
	Params       string // hex encoded

	// results
	GasUsed         int64
	ExecutionStatus int
	ResultDataUri   string
	LogDataUri      string

	Status     ExecutionTaskStatus
	CreateTime int64
	UpdateTime int64
}

func (ExecutionTask) TableName() string {
	return "execution_task"
}

func InitTables(db *gorm.DB) {
	if !db.HasTable(&BlockLog{}) {
		db.CreateTable(&BlockLog{})
		db.Model(&BlockLog{}).AddUniqueIndex("idx_block_log_height", "height")
		db.Model(&BlockLog{}).AddIndex("idx_block_log_create_time", "create_time")
	}

	if !db.HasTable(&EventLog{}) {
		db.CreateTable(&EventLog{})
	}

	if !db.HasTable(&ExecutionTask{}) {
		db.CreateTable(&ExecutionTask{})
	}
}
