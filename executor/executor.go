package executor

import (
	"archive/zip"
	"fmt"
	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/jinzhu/gorm"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Executor struct {
	DB            *gorm.DB
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
	archive, err := zip.OpenReader(fname)
	if err != nil {
		panic(err)
	}
	defer archive.Close()

	for _, f := range archive.File {
		filePath := filepath.Join(dst, f.Name)
		fmt.Println("unzipping file ", filePath)

		if !strings.HasPrefix(filePath, filepath.Clean(dst)+string(os.PathSeparator)) {
			fmt.Println("invalid file path")
			return
		}
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
func NewExecutor(db *gorm.DB) *Executor {
	return &Executor{db, 0, Receipt{
		gasUsed:    0,
		returnCode: "",
		resultUri:  "",
		logUri:     "",
	}}
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
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id >= ", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Order("task_id asc").Take(&executionTask).Error()
	if err != nil {
		return
	}
	ex.currentTaskId = executionTask.TaskId
	// 2. download binary and data
	err = ex.downloadExecutable(executionTask.ExecutionUri)
	if err != nil {
		return
	}

	err = ex.downloadInputFiles(executionTask.InputFiles)
	if err != nil {
		return
	}

	args := []string{"./inputs/data.txt", "./outputs/result.txt"}
	// 3. invoke iwasm - original design is to launch docker. here we directly start wasm runtime instead for PoC
	cmd := exec.Command("./iwasm", args...)
	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	err = cmd.Run()

	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		fmt.Print(string(tmp))
		if err != nil {
			break
		}
	}

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

func (ex *Executor) downloadExecutable(uri string) error {
	return nil
}

func (ex *Executor) downloadInputFiles(files string) error {
	return nil
}

func (ex *Executor) uploadResultsAndLogs() error {
	ex.receipt.resultUri = ""
	ex.receipt.logUri = ""
	return nil
}

func (ex *Executor) writeReceipt() error {
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id == ", model.ExecutionTaskStatusStatusInit,
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
