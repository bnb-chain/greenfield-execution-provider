package sender

import (
	"context"
	"time"

	"cosmossdk.io/math"
	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield/sdk/types"
	"github.com/jinzhu/gorm"

	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/util"
)

type Sender struct {
	DB        *gorm.DB
	sdkClient sdkclient.Client
}

func NewSender(db *gorm.DB, client sdkclient.Client) *Sender {
	return &Sender{
		DB:        db,
		sdkClient: client,
	}
}

func (s *Sender) Start() {
	go s.send()
}

func (s *Sender) send() {
	for {
		time.Sleep(common.SenderSendInterval)

		task, err := s.getResultToSubmit()
		if err != nil {
			continue
		}

		res, err := s.sdkClient.SubmitExecutionResult(context.Background(), math.NewUint(uint64(task.TaskId)), uint32(0 /*task.ExecutionStatus*/), task.ResultDataUri, types.TxOption{})
		if err != nil {
			util.Logger.Errorf("submit execution result error: %s", err.Error())
			continue
		}

		util.Logger.Infof("submit execution result success, txHash=%s", res.TxHash)

		err = s.DB.Model(&model.ExecutionTask{}).Where("task_id = ?", task.TaskId).Updates(map[string]interface{}{
			"status":         model.ExecutionTaskStatusStatusReceiptSubmitted,
			"submit_tx_hash": res.TxHash,
		}).Error
		if err != nil {
			util.Logger.Errorf("update execution task status error: %s", err.Error())
			continue
		}
	}
}

func (s *Sender) getResultToSubmit() (*model.ExecutionTask, error) {
	task := model.ExecutionTask{}
	err := s.DB.Where("status = ?", model.ExecutionTaskStatusStatusExecuted).Order("task_id asc").Take(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}
