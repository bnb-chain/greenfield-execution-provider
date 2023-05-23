package observer

import (
	"fmt"
	"time"

	"github.com/binance-chain/greenfield-execution-provider/client"
	"github.com/binance-chain/greenfield-execution-provider/common"
	"github.com/binance-chain/greenfield-execution-provider/model"
	"github.com/binance-chain/greenfield-execution-provider/util"
	"github.com/jinzhu/gorm"
)

type Observer struct {
	DB     *gorm.DB
	Config *util.ObserverConfig
	Client *client.GreenfieldClient
}

// NewObserver returns the observer instance
func NewObserver(db *gorm.DB, cfg *util.ObserverConfig, client *client.GreenfieldClient) *Observer {
	return &Observer{
		DB:     db,
		Config: cfg,
		Client: client,
	}
}

// Start starts the routines of observer
func (ob *Observer) Start() {
	go ob.Fetch(ob.Config.GreenfieldConfig.StartHeight)
	go ob.ProcessConfirmedEvent()
	go ob.PruneBlocks()
	go ob.Alert()
}

// Fetch starts the main routine for fetching blocks of BSC
func (ob *Observer) Fetch(startHeight int64) {
	for {
		curBlockLog, err := ob.GetCurrentBlockLog()
		if err != nil {
			util.Logger.Errorf("get current block log error, err=%s", err.Error())
			time.Sleep(common.ObserverFetchInterval)
			continue
		}

		nextHeight := curBlockLog.Height + 1
		if curBlockLog.Height == 0 && startHeight != 0 {
			nextHeight = startHeight
		}

		util.Logger.Infof("fetch block, height=%d", nextHeight)
		err = ob.fetchBlock(curBlockLog.Height, nextHeight, curBlockLog.BlockHash)
		if err != nil {
			util.Logger.Errorf("fetch block error, err=%s", err.Error())
			time.Sleep(common.ObserverFetchInterval)
		}
	}
}

// fetchBlock fetches the next block of BSC and saves it to database. if the next block hash
// does not match to the parent hash, the current block will be deleted for there is a fork.
func (ob *Observer) fetchBlock(curHeight, nextHeight int64, curBlockHash string) error {
	blockAndEventLogs, err := ob.Client.GetBlockAndEventsAtHeight(nextHeight)
	if err != nil {
		return fmt.Errorf("get block info error, height=%d, err=%s", nextHeight, err.Error())
	}

	parentHash := blockAndEventLogs.ParentBlockHash
	if curHeight != 0 && parentHash != curBlockHash {
		return ob.DeleteBlockAndEvents(curHeight)
	} else {
		nextBlockLog := model.BlockLog{
			BlockHash:  blockAndEventLogs.BlockHash,
			ParentHash: parentHash,
			Height:     blockAndEventLogs.Height,
			BlockTime:  blockAndEventLogs.BlockTime,
		}

		err := ob.SaveBlockAndEvents(&nextBlockLog, blockAndEventLogs.Events)
		if err != nil {
			return err
		}

		err = ob.UpdateConfirmedNum(nextBlockLog.Height)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteBlockAndEvents deletes the block and txs of the given height
func (ob *Observer) DeleteBlockAndEvents(height int64) error {
	tx := ob.DB.Begin()
	if err := tx.Error; err != nil {
		return err
	}

	if err := tx.Where("height = ?", height).Delete(model.BlockLog{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Where("height = ? and status = ?", height, model.EventStatusInit).Delete(model.EventLog{}).Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// UpdateConfirmedNum updates confirmation number of events
func (ob *Observer) UpdateConfirmedNum(height int64) error {
	err := ob.DB.Model(model.EventLog{}).Where("status = ?", model.EventStatusInit).Updates(
		map[string]interface{}{
			"confirmed_num": gorm.Expr("? - height", height+1),
			"update_time":   time.Now().Unix(),
		}).Error
	if err != nil {
		return err
	}

	err = ob.DB.Model(model.EventLog{}).Where("status = ? and confirmed_num >= ?",
		model.EventStatusInit, common.DefaultConfirmNum).Updates(
		map[string]interface{}{
			"status":      model.EventStatusConfirmed,
			"update_time": time.Now().Unix(),
		}).Error
	if err != nil {
		return err
	}

	return nil
}

// PruneBlocks prunes the outdated blocks
func (ob *Observer) PruneBlocks() {
	for {
		time.Sleep(common.ObserverPruneInterval)

		curBlockLog, err := ob.GetCurrentBlockLog()
		if err != nil {
			util.Logger.Errorf("get current block log error, err=%s", err.Error())
			continue
		}

		err = ob.DB.Where("height < ?", curBlockLog.Height-common.ObserverMaxBlockNumber).Delete(model.BlockLog{}).Error
		if err != nil {
			util.Logger.Infof("prune block logs error, err=%s", err.Error())
		}
	}
}

func (ob *Observer) ProcessConfirmedEvent() {
	go ob.processConfirmedEvent(common.ExecutionTaskEvent)
	go ob.processConfirmedEvent(common.ExecutionResultEvent)
}

func (ob *Observer) processConfirmedEvent(eventType string) {
	for {
		time.Sleep(common.ObserverFetchInterval)

		eventLog := model.EventLog{}
		err := ob.DB.Where("status = ? and event_name = ?", model.EventStatusConfirmed, eventType).Order("task_id asc").Take(&eventLog).Error
		if err != nil {
			continue
		}

		switch eventType {
		case common.ExecutionTaskEvent:
			err := ob.processExecutionTask(eventLog)
			if err != nil {
				util.Logger.Errorf("process execution task error, err=%s", err.Error())
				continue
			}
		}
	}
}

func (ob *Observer) processExecutionTask(eventLog model.EventLog) error {
	taskModel := &model.ExecutionTask{
		InvokeTxHash:      eventLog.TxHash,
		TaskId:            eventLog.TaskId,
		ExecutionObjectId: eventLog.ExecutableObjectId,
		ExecutionUri:      "", // todo
		InputFiles:        eventLog.InputObjectIds,
		MaxGas:            eventLog.MaxGas,
		InvokeMethod:      eventLog.Method,
		Params:            eventLog.Params,
	}

	tx := ob.DB.Begin()
	if err := tx.Error; err != nil {
		util.Logger.Errorf("start transaction error, err=%s", err.Error())
		return err
	}

	err := tx.Model(&eventLog).Updates(
		map[string]interface{}{
			"status":      model.EventStatusProcessed,
			"update_time": time.Now().Unix(),
		}).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Create(taskModel).Error; err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit().Error
	if err != nil {
		util.Logger.Errorf("commit transaction error, err=%s", err.Error())
		return err
	}
	return nil
}

// SaveBlockAndEvents saves block and packages to database
func (ob *Observer) SaveBlockAndEvents(blockLog *model.BlockLog, packages []interface{}) error {
	tx := ob.DB.Begin()
	if err := tx.Error; err != nil {
		return err
	}

	if err := tx.Create(blockLog).Error; err != nil {
		tx.Rollback()
		return err
	}

	for _, pack := range packages {
		if err := tx.Create(pack).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

// GetCurrentBlockLog returns the highest block log
func (ob *Observer) GetCurrentBlockLog() (*model.BlockLog, error) {
	blockLog := model.BlockLog{}
	err := ob.DB.Order("height desc").Take(&blockLog).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return &blockLog, nil
}

// Alert sends alerts to tg group if there is no new block fetched in a specific time
func (ob *Observer) Alert() {
	for {
		curChainBlockLog, err := ob.GetCurrentBlockLog()
		if err != nil {
			util.Logger.Errorf("get current block log error, err=%s", err.Error())
			time.Sleep(common.ObserverAlertInterval)

			continue
		}
		if curChainBlockLog.Height > 0 {
			if time.Now().Unix()-curChainBlockLog.CreateTime > ob.Config.AlertConfig.BlockUpdateTimeout {
				msg := fmt.Sprintf("[%s] last greenfield block fetched at %s, height=%d",
					ob.Config.AlertConfig.Moniker, time.Unix(curChainBlockLog.CreateTime, 0).String(), curChainBlockLog.Height)
				util.SendSlackMessage(msg)
			}
		}

		time.Sleep(common.ObserverAlertInterval)
	}
}
