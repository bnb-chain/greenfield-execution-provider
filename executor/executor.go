package executor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/util"
	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield-go-sdk/types"
	"github.com/jinzhu/gorm"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const downloadDir = "download"
const executableZip = "./wordcount.zip"
const inputZip = "./input.zip"
const outputDir = "./output"

// reuse the one for executable
var outputBucketName string

type Executor struct {
	DB            *gorm.DB
	Client        sdkclient.Client
	currentTaskId int64
	receipt       Receipt
}

type Receipt struct {
	gasUsed        uint64
	returnCode     string
	resultObjectId string
	logObjectId    string
}

type ExecutionReport struct {
	GasUsed   uint64 `json:"gasUsed"`
	ResultMsg string `json:"resultMsg"`
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
			gasUsed:        0,
			returnCode:     "",
			resultObjectId: "",
			logObjectId:    "",
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
	maxGasOption := "--max-gas=" + executionTask.MaxGas
	args := []string{maxGasOption, "./wordcount/word_count.wasm", "./input/data.txt", "./output/result.txt"}
	// 3. invoke iwasm - original design is to launch docker. here we directly start wasm runtime instead for PoC
	cmd := exec.Command("./iwasm", args...)
	// err = cmd.Run()

	output, _ := cmd.CombinedOutput()
	//fmt.Println("Command output: ", string(output))
	writeBytesToFile("./output/log.txt", output)
	/*
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
	*/
	executeReport, err := readExecuteReport("./report.json")
	if err != nil {
		fmt.Println(err)
		ex.receipt.returnCode = err.Error()
	}
	ex.receipt.returnCode = executeReport.ResultMsg
	ex.receipt.gasUsed = executeReport.GasUsed

	fmt.Printf("result: gasUsed %d, returnCode %s\n", ex.receipt.gasUsed, ex.receipt.returnCode)
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

func readExecuteReport(reportJson string) (ExecutionReport, error) {

	report := ExecutionReport{0, "nil"}
	// Open report.json
	jsonFile, err := os.Open(reportJson)
	if err != nil {
		return report, err
	}
	fmt.Println("Successfully Opened " + reportJson)
	// defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return report, err
	}

	err = json.Unmarshal(byteValue, &report)
	return report, err
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

func (ex *Executor) downloadObject(objectId string) (string, error) {
	// create download dir
	_ = os.Mkdir(downloadDir, os.ModePerm)

	objectInfo, err := ex.Client.HeadObjectByID(context.Background(), objectId)
	if err != nil {
		return "", err
	}

	// remember the executable object bucket name for result upload
	outputBucketName = objectInfo.BucketName

	ior, _, err := ex.Client.GetObject(context.Background(), objectInfo.BucketName, objectInfo.ObjectName, types.GetObjectOption{})
	if err != nil {
		return "", err
	}

	bts, err := io.ReadAll(ior)
	if err != nil {
		return "", err
	}
	filepath := fmt.Sprintf("./%s/%s", downloadDir, objectInfo.ObjectName)
	err = os.WriteFile(filepath, bts, 0644)
	if err != nil {
		return "", err
	}
	return filepath, err
}

func (ex *Executor) downloadExecutable(objectId string) error {
	util.Logger.Infof("try to download executable, objectId=%s", objectId)
	executableZip, err := ex.downloadObject(objectId)
	if err != nil {
		util.Logger.Errorf("download executable failed, err=%s", err.Error())
		return err
	}
	unzipFile(executableZip, "./")
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
		inputPath, err := ex.downloadObject(objectId)
		if err != nil {
			return err
		}
		if strings.HasSuffix(inputPath, ".zip") {
			unzipFile(inputPath, "./")
		}
	}
	return nil
}

func (ex *Executor) uploadFile(dir string, fileName string) (string, error) {
	// read file
	filepath := dir + "/" + fileName
	dataBuf, err := os.ReadFile(filepath)
	if err != nil {
		fmt.Println("Can not read input file: " + filepath)
		return "", err
	}

	fmt.Printf("---> CreateObject (%s) and HeadObject into bucket (%s) <---\n", fileName, outputBucketName)
	uploadTx, err := ex.Client.CreateObject(context.Background(), outputBucketName, fileName, bytes.NewReader(dataBuf), types.CreateObjectOptions{})
	if err != nil {
		fmt.Println("Error create object: " + err.Error())
		return "", err
	}
	_, err = ex.Client.WaitForTx(context.Background(), uploadTx)
	if err != nil {
		fmt.Println("Error wait TX: " + err.Error())
		return "", err
	}

	time.Sleep(5 * time.Second)

	fmt.Printf("---> PutObject (%s) <---\n", fileName)
	err = ex.Client.PutObject(context.Background(), outputBucketName, fileName, int64(len(dataBuf)),
		bytes.NewReader(dataBuf), types.PutObjectOptions{})
	if err != nil {
		fmt.Println("Error put object: " + err.Error())
		return "", err
	}
	time.Sleep(10 * time.Second)
	dataObjectInfo, err := ex.Client.HeadObject(context.Background(), outputBucketName, fileName)
	if err != nil {
		fmt.Println("Error HeadObject: " + err.Error())
		return "", err
	}
	fmt.Printf("Upload object %s, get ObjectID %s\n", filepath, dataObjectInfo.Id.String())
	return dataObjectInfo.Id.String(), nil
}

func (ex *Executor) uploadResultsAndLogs() error {

	resultObjectId, err := ex.uploadFile(outputDir, "result.txt")
	if err != nil {
		return err
	}
	logObjectId, err := ex.uploadFile(outputDir, "log.txt")

	ex.receipt.resultObjectId = resultObjectId
	ex.receipt.logObjectId = logObjectId
	return err
}

func (ex *Executor) writeReceipt() error {
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id = ?", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Updates(
		map[string]interface{}{
			"status":           model.ExecutionTaskStatusStatusExecuted,
			"gas_used":         ex.receipt.gasUsed,
			"execution_status": ex.receipt.returnCode,
			"result_data_uri":  ex.receipt.resultObjectId,
			"log_data_uri":     ex.receipt.logObjectId,
		}).Error

	if err != nil {
		fmt.Println("Fail to update executed task")
		return err
	}

	err = ex.DB.Commit().Error
	if err != nil {
		util.Logger.Errorf("commit executed task error, err=%s", err.Error())
		return err
	}

	return err
}
