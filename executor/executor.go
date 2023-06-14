package executor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/jinzhu/gorm"

	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/util"
	sdkclient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield-go-sdk/types"
)

const downloadDir = "download"
const outputDir = "./output"
const inputPath = "./input"

// reuse the one for executable
var outputBucketName string
var executablePath string

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
	// fmt.Printf("trying to find execution task with taskID > %v\n", ex.currentTaskId)
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id > ?", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Order("task_id asc").Take(&executionTask).Error
	if err != nil {
		// fmt.Println("tryInvokeExecuteTake error " + err.Error())
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

	max_gas_env := "MAX_GAS=" + executionTask.MaxGas
	// TODO: use package.json to identify the path
	wasm_file_env := "WASM_FILE=" + "./wordcount/word_count.wasm"
	input_file_env := "INPUT_FILES=" + inputPath + "/data.txt"
	output_file_env := "OUTPUT_FILES=" + outputDir + "/result.txt"
	argsEnv := []string{max_gas_env, wasm_file_env, input_file_env, output_file_env}

	abs_dir, err := filepath.Abs("./")

	wasm_mount_dir := abs_dir + "/wordcount"
	input_mount_dir := abs_dir + "/input"
	output_mount_dir := abs_dir + "/output"
	if err != nil {
		panic(err)
	}

	// Pull Docker image
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	reader, err := cli.ImagePull(ctx, "sunny2022za/gnfdexe:latest", dockertypes.ImagePullOptions{})
	if err != nil {
		panic(err)
	}

	defer reader.Close()
	io.Copy(os.Stdout, reader)

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "sunny2022za/gnfdexe",
		Env:   argsEnv,
		Tty:   false,
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: wasm_mount_dir,
				Target: "/opt/gnfd/workdir/wordcount",
			},
			{
				Type:   mount.TypeBind,
				Source: input_mount_dir,
				Target: "/opt/gnfd/workdir/input",
			},
			{
				Type:   mount.TypeBind,
				Source: output_mount_dir,
				Target: "/opt/gnfd/workdir/output",
			},
		},
	}, nil, nil, "")
	if err != nil {
		panic(err)
	}
	fmt.Println("start Container " + resp.ID)
	if err := cli.ContainerStart(ctx, resp.ID, dockertypes.ContainerStartOptions{}); err != nil {
		fmt.Println(err.Error())
		panic(err)
	}
	fmt.Println("wait Container " + resp.ID)
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			fmt.Println(err.Error())
			panic(err)
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, dockertypes.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}
	//stdcopy.StdCopy(os.Stdout, os.Stderr, out)
	f, err := os.Create("./output/log.txt")
	io.Copy(f, out)

	/*
		maxGasOption := "--max-gas=" + executionTask.MaxGas
		args := []string{maxGasOption, "./wordcount/word_count.wasm", "./input/data.txt", "./output/result.txt"}
		// 3. invoke iwasm - original design is to launch docker. here we directly start wasm runtime instead for PoC
		cmd := exec.Command("./iwasm", args...)
		// err = cmd.Run()

		output, _ := cmd.CombinedOutput()
		//fmt.Println("Command output: ", string(output))
		writeBytesToFile("./output/log.txt", output)

	*/
	executeReport, err := readExecuteReport("./output/report.json")
	if err != nil {
		fmt.Println(err)
		ex.receipt.returnCode = err.Error()
	}
	ex.receipt.returnCode = executeReport.ResultMsg
	ex.receipt.gasUsed = executeReport.GasUsed

	fmt.Printf("result: gasUsed %d, returnCode %s\n", ex.receipt.gasUsed, ex.receipt.returnCode)
	// 4. stop and destroy container
	stopAndRemoveContainer(ctx, cli, resp.ID)

	// 5. upload result data and logs
	err = ex.uploadResultsAndLogs()
	if err != nil {
		return
	}
	// 6. write receipt into db
	err = ex.writeReceipt()
	if err != nil {
		return
	}

	return
}

func stopAndRemoveContainer(ctx context.Context, cli *client.Client, id string) {
	fmt.Println("stop and destroy container: " + id)
	if err := cli.ContainerStop(ctx, id, nil); err != nil {
		panic(err)
	}
	removeOptions := dockertypes.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}

	if err := cli.ContainerRemove(ctx, id, removeOptions); err != nil {
		fmt.Printf("Unable to remove container: %s\n", err)
	}
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

	return err
}
