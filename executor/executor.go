package executor

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield-go-sdk/types"
	"github.com/jinzhu/gorm"

	"github.com/bnb-chain/greenfield-execution-provider/util"

	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
)

const (
	downloadDir = "download"
)

const executableZip = "./wordcount.zip"
const inputZip = "./input.zip"
const outputDir = "./output"

type Executor struct {
	DB            *gorm.DB
	Client        sdkclient.Client
	currentTaskId int64
	receipt       Receipt
}

type Receipt struct {
	gasUsed    int64
	returnCode string
	resultUri  string
	logUri     string
}

func unzipFile(fname string, dst string) {
	fmt.Printf("try to unzip file %s to %s\n", fname, dst)
	archive, err := zip.OpenReader(fname)
	if err != nil {
		panic(err)
	}
	defer archive.Close()

	for _, f := range archive.File {
		filePath := filepath.Join(dst, f.Name)
		fmt.Println("unzipping file ", filePath)

		/*if !strings.HasPrefix(filePath, filepath.Clean(dst)+string(os.PathSeparator)) {
			fmt.Println("invalid file path")
			return
		}*/
		if f.FileInfo().IsDir() {
			fmt.Println("creating directory...")
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			panic(err)
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			panic(err)
		}

		fileInArchive, err := f.Open()
		if err != nil {
			panic(err)
		}

		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			panic(err)
		}

		dstFile.Close()
		fileInArchive.Close()
	}
}

// NewExecutor returns the executor instance
func NewExecutor(db *gorm.DB, client sdkclient.Client) *Executor {
	return &Executor{
		db,
		client,
		0,
		Receipt{
			gasUsed:    0,
			returnCode: "",
			resultUri:  "",
			logUri:     "",
		},
	}
}

// Start starts the routines of executor
func (ex *Executor) Start() {
	for {
		time.Sleep(common.ExecutorFetchInterval)
		go ex.tryInvokeExecuteTask()
	}
}

func (ex *Executor) tryInvokeExecuteTask() {
	// 1. load executeTask from db, compare the taskID
	executionTask := model.ExecutionTask{}
	fmt.Printf("trying to find execution task with taskID > %v\n", ex.currentTaskId)
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id > ?", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Order("task_id asc").Take(&executionTask).Error
	if err != nil {
		fmt.Println("tryInvokeExecuteTake error " + err.Error())
		return
	} else {
		fmt.Println("find executionTask: " + executionTask.ExecutionObjectId)
	}
	ex.currentTaskId = executionTask.TaskId
	// 2. download binary and data
	err = ex.downloadExecutable(executionTask.ExecutionObjectId)
	if err != nil {
		return
	}

	err = ex.downloadInputFiles(executionTask.InputFiles)
	if err != nil {
		return
	}

	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		panic(err)
	}
	args := []string{"./wordcount/word_count.wasm", "./input/data.txt", "./output/result.txt"}
	// 3. invoke iwasm - original design is to launch docker. here we directly start wasm runtime instead for PoC
	cmd := exec.Command("./iwasm", args...)
	// err = cmd.Run()

	output, _ := cmd.CombinedOutput()
	//fmt.Println("Command output: ", string(output))
	writeBytesToFile("./output/log.txt", output)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			ex.receipt.returnCode = exitError.Error()
			fmt.Printf("Command exited with return code: %d\n", ex.receipt.returnCode)
		} else {
			ex.receipt.returnCode = "Unknown error"
			fmt.Printf("Command exited with unknown error!")
		}
	} else {
		ex.receipt.returnCode = "Success"
		fmt.Printf("Command exited successfully.")
	}

	// fixme! fake gas.
	ex.receipt.gasUsed = 1023
	// 4. upload result data and logs
	err = ex.uploadResultsAndLogs()
	if err != nil {
		return
	}
	// 5. write receipt into db
	err = ex.writeReceipt()
	if err != nil {
		return
	}
	// 6. return
	return
}

func writeBytesToFile(file string, output []byte) {
	f, err := os.Create(file)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
	}()

	num, err := f.Write(output)
	if err != nil {
		panic(err)
	}
	fmt.Printf("write %d bytes to file %s\n", num, file)
}

func (ex *Executor) downloadObject(objectId string) error {
	// create download dir
	_ = os.Mkdir(downloadDir, os.ModePerm)

	objectInfo, err := ex.Client.HeadObjectByID(context.Background(), objectId)
	if err != nil {
		return err
	}

	ior, _, err := ex.Client.GetObject(context.Background(), objectInfo.BucketName, objectInfo.ObjectName, types.GetObjectOption{})
	if err != nil {
		return err
	}

	bts, err := io.ReadAll(ior)
	if err != nil {
		return err
	}

	err = os.WriteFile(fmt.Sprintf("./%s/%s", downloadDir, objectId), bts, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (ex *Executor) downloadExecutable(objectId string) error {
	util.Logger.Infof("try to download executable, objectId=%s", objectId)
	err := ex.downloadObject(objectId)
	if err != nil {
		util.Logger.Errorf("download executable failed, err=%s", err.Error())
		return err
	}
	//unzipFile(executableZip, "./")
	return nil
}

func (ex *Executor) downloadInputFiles(objectIds string) error {
	util.Logger.Infof("try to download inputs, objects=%s", objectIds)
	inputObjects := make([]string, 0)
	err := json.Unmarshal([]byte(objectIds), &inputObjects)
	if err != nil {
		return err
	}

	for _, objectId := range inputObjects {
		err = ex.downloadObject(objectId)
		if err != nil {
			return err
		}
	}

	//unzipFile(inputZip, "./")
	return nil
}

func (ex *Executor) uploadResultsAndLogs() error {
	ex.receipt.resultUri = ""
	ex.receipt.logUri = ""
	return nil
}

func (ex *Executor) writeReceipt() error {
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id == ?", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Updates(
		map[string]interface{}{
			"status":           model.ExecutionTaskStatusStatusExecuted,
			"gas_used":         ex.receipt.gasUsed,
			"execution_status": ex.receipt.returnCode,
			"result_data_uri":  ex.receipt.resultUri,
			"log_data_uri":     ex.receipt.logUri,
		}).Error
	return err
}
