package client

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/binance-chain/greenfield-execution-provider/common"
	"github.com/binance-chain/greenfield-execution-provider/model"
	"github.com/binance-chain/greenfield-execution-provider/util"
	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	sdktypes "github.com/bnb-chain/greenfield-go-sdk/types"
	"github.com/bnb-chain/greenfield/sdk/client"
)

type GreenfieldClient struct {
	config    *util.GreenfieldConfig
	sdkClient sdkclient.Client
	tmClient  client.TendermintClient
}

func NewGreenFieldClient(cfg *util.GreenfieldConfig) *GreenfieldClient {
	var account *sdktypes.Account = nil
	var err error
	if cfg.PrivateKey != "" {
		account, err = sdktypes.NewAccountFromPrivateKey("sender", cfg.PrivateKey)
		if err != nil {
			panic(err)
		}
	}

	sdkClient, err := sdkclient.New(cfg.ChainIdString, cfg.RPCAddr, sdkclient.Option{DefaultAccount: account})
	if err != nil {
		panic(err)
	}

	tmClient := client.NewTendermintClient(cfg.RPCAddr)

	return &GreenfieldClient{
		config:    cfg,
		sdkClient: sdkClient,
		tmClient:  tmClient,
	}
}

func (c *GreenfieldClient) GetBlockAndEventsAtHeight(height int64) (*common.BlockAndEventLogs, error) {
	result := &common.BlockAndEventLogs{}

	block, err := c.tmClient.TmClient.Block(context.Background(), &height)
	if err != nil {
		return nil, err
	}
	result.Height = block.Block.Height
	result.BlockHash = block.Block.Hash().String()
	result.ParentBlockHash = block.Block.LastBlockID.Hash.String()
	result.BlockTime = block.Block.Time.Unix()

	blockResults, err := c.tmClient.TmClient.BlockResults(context.Background(), &height)
	if err != nil {
		return nil, err
	}

	for idx, tx := range blockResults.TxsResults {
		for _, event := range tx.Events {
			switch event.Type {
			case common.ExecutionTaskEvent:
				eventLog := &model.EventLog{
					EventName: common.ExecutionTaskEvent,
					BlockHash: result.BlockHash,
					TxHash:    strings.ToUpper(hex.EncodeToString(block.Block.Txs[idx].Hash())),
					Height:    result.Height,
				}

				for _, attr := range event.Attributes {
					switch attr.Key {
					case "task_id":
						taskId, err := util.QuotedStrToIntWithBitSize(attr.Value, 64)
						if err != nil {
							return nil, err
						}
						eventLog.TaskId = taskId
					case "operator":
						operator, err := strconv.Unquote(attr.Value)
						if err != nil {
							return nil, err
						}
						eventLog.Operator = operator
					case "executable_object_id":
						executableObjectId, err := strconv.Unquote(attr.Value)
						if err != nil {
							return nil, err
						}
						eventLog.ExecutableObjectId = executableObjectId
					case "input_object_ids":
						//todo: process this
						eventLog.InputObjectIds = attr.Value
					case "max_gas":
						maxGas, err := strconv.Unquote(attr.Value)
						if err != nil {
							return nil, err
						}
						eventLog.MaxGas = maxGas
					case "method":
						method, err := strconv.Unquote(attr.Value)
						if err != nil {
							return nil, err
						}
						eventLog.Method = method
					case "params":
						params, err := strconv.Unquote(attr.Value)
						if err != nil {
							return nil, err
						}
						bts, err := base64.StdEncoding.DecodeString(params)
						if err != nil {
							return nil, err
						}
						eventLog.Params = hex.EncodeToString(bts)
					}
				}

				result.Events = append(result.Events, eventLog)
			case common.ExecutionResultEvent:
				eventLog := &model.EventLog{
					EventName: common.ExecutionResultEvent,
					BlockHash: result.BlockHash,
					TxHash:    strings.ToUpper(hex.EncodeToString(block.Block.Txs[idx].Hash())),
					Height:    result.Height,
				}
				result.Events = append(result.Events, eventLog)
			}
		}
	}

	return result, nil
}
