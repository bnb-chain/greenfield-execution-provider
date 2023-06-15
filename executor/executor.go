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

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/jinzhu/gorm"

	"github.com/bnb-chain/greenfield-execution-provider/common"
	"github.com/bnb-chain/greenfield-execution-provider/model"
	"github.com/bnb-chain/greenfield-execution-provider/util"
	sdkClient "github.com/bnb-chain/greenfield-go-sdk/client"
	"github.com/bnb-chain/greenfield-go-sdk/types"
)

const downloadDir = "download"
const executableConfigFileName = "ExecutableConfig.json"

var outputDir string
var outputFilesName []string
var inputDir string
var inputFilesName []string

var outputBucketName string

type Executor struct {
	DB            *gorm.DB
	Client        sdkClient.Client
	currentTaskId int64
	receipt       Receipt
}

type Receipt struct {
	gasUsed        uint64
	returnCode     string
	resultObjectId string
	logObjectId    string
}

type ExecutableConfig struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Author           string `json:"author"`
	BinaryDigest     string `json:"binaryDigest"`
	SourceCodeFile   string `json:"sourceCodeFile"`
	SourceCodeDigest string `json:"sourceCodeDigest"`
	Abi              struct {
		Entry     string `json:"entry"`
		Signature string `json:"signature"`
	} `json:"abi"`
	Executable struct {
		WasmMainFile  string `json:"wasmMainFile"`
		WasmLibraries string `json:"wasmLibraries"`
	} `json:"executable"`
	Data struct {
		InputDir    string   `json:"inputDir"`
		InputFiles  []string `json:"inputFiles"`
		OutputDir   string   `json:"outputDir"`
		LogDir      string   `json:"logDir"`
		OutputFiles []string `json:"outputFiles"`
	} `json:"data"`
	Capabilities struct {
		FileOps struct {
			NativeFile struct {
				Read   bool `json:"read"`
				Write  bool `json:"write"`
				Create bool `json:"create"`
			} `json:"nativeFile"`
		} `json:"fileOps"`
	} `json:"capabilities"`
}

type ExecutionReport struct {
	GasUsed   uint64 `json:"gasUsed"`
	ResultMsg string `json:"resultMsg"`
}

func unzipFile(fileName string, dst string) {
	util.Logger.Infof("try to unzip file %s to %s\n", fileName, dst)
	archive, err := zip.OpenReader(fileName)
	if err != nil {
		panic(err)
	}
	defer archive.Close()

	for _, f := range archive.File {
		filePath := filepath.Join(dst, f.Name)
		util.Logger.Info("unzipping file ", filePath)

		if f.FileInfo().IsDir() {
			util.Logger.Info("creating directory..." + filePath)
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
func NewExecutor(db *gorm.DB, client sdkClient.Client) *Executor {
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
	err := ex.DB.Model(&model.ExecutionTask{}).Where("status = ? and task_id > ?", model.ExecutionTaskStatusStatusInit,
		ex.currentTaskId).Order("task_id asc").Take(&executionTask).Error
	if err != nil {
		util.Logger.Error("tryInvokeExecuteTake error " + err.Error())
		return
	} else {
		util.Logger.Error("find executionTask: " + executionTask.ExecutionObjectId)
	}
	ex.currentTaskId = executionTask.TaskId
	// 2. download binary and data
	executableConfig, execDir, err := ex.downloadExecutable(executionTask.ExecutionObjectId)
	if err != nil {
		return
	}

	err = ex.downloadInputFiles(executionTask.InputFiles, executableConfig)
	if err != nil {
		return
	}

	outputDir = executableConfig.Data.OutputDir
	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		panic(err)
	}

	maxGasEnv := "MAX_GAS=" + executionTask.MaxGas
	// TODO: use ExecutableConfig.json to identify the path

	wasmFileEnv := "WASM_FILE=" + filepath.Base(execDir) + "/" + executableConfig.Executable.WasmMainFile

	inputDir = executableConfig.Data.InputDir
	inputFilesName = executableConfig.Data.InputFiles
	var inputFiles string
	for i := 0; i < len(inputFilesName); i++ {
		inputFiles += inputDir + "/" + inputFilesName[i] + " "
	}
	inputFileEnv := "INPUT_FILES=" + inputFiles

	outputFilesName = executableConfig.Data.OutputFiles
	var outputFiles string
	for j := 0; j < len(outputFilesName); j++ {
		util.Logger.Infof("generate output files: %s/%s\n", outputDir, outputFilesName[j])
		outputFiles += outputDir + "/" + outputFilesName[j] + " "
	}
	outputFileEnv := "OUTPUT_FILES=" + outputFiles
	argsEnv := []string{maxGasEnv, wasmFileEnv, inputFileEnv, outputFileEnv}

	abs, err := filepath.Abs("./")
	wasmMountDir, _ := filepath.Abs(execDir)
	inputMountDir := abs + "/" + inputDir
	outputMountDir := abs + "/" + outputDir

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

	reader, err := cli.ImagePull(ctx, "gnfdexec/gnfdexe:latest", dockerTypes.ImagePullOptions{})
	if err != nil {
		panic(err)
	}

	defer reader.Close()
	io.Copy(os.Stdout, reader)

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "gnfdexec/gnfdexe",
		Env:   argsEnv,
		Tty:   false,
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: wasmMountDir,
				Target: "/opt/gnfd/workdir/" + filepath.Base(execDir),
			},
			{
				Type:   mount.TypeBind,
				Source: inputMountDir,
				Target: "/opt/gnfd/workdir/input",
			},
			{
				Type:   mount.TypeBind,
				Source: outputMountDir,
				Target: "/opt/gnfd/workdir/output",
			},
		},
	}, nil, nil, "")
	if err != nil {
		panic(err)
	}
	util.Logger.Infof("start Container " + resp.ID)
	if err := cli.ContainerStart(ctx, resp.ID, dockerTypes.ContainerStartOptions{}); err != nil {
		util.Logger.Errorf(err.Error())
		panic(err)
	}
	util.Logger.Infof("wait Container " + resp.ID)
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			util.Logger.Errorf(err.Error())
			panic(err)
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, dockerTypes.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}
	f, err := os.Create(outputDir + "/log.txt")
	io.Copy(f, out)
	executeReport, err := readExecuteReport("./output/report.json")
	if err != nil {
		util.Logger.Errorf(err.Error())
		ex.receipt.returnCode = err.Error()
	}
	ex.receipt.returnCode = executeReport.ResultMsg
	ex.receipt.gasUsed = executeReport.GasUsed
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
	util.Logger.Infof("stop and destroy container: " + id)
	if err := cli.ContainerStop(ctx, id, nil); err != nil {
		panic(err)
	}
	removeOptions := dockerTypes.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}

	if err := cli.ContainerRemove(ctx, id, removeOptions); err != nil {
		util.Logger.Errorf("Unable to remove container: %s\n", err)
	}
}

func readExecuteReport(reportJson string) (ExecutionReport, error) {

	report := ExecutionReport{0, "nil"}
	// Open report.json
	jsonFile, err := os.Open(reportJson)
	if err != nil {
		return report, err
	}

	// defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return report, err
	}

	err = json.Unmarshal(byteValue, &report)
	return report, err
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
	objectPath := fmt.Sprintf("./%s/%s", downloadDir, objectInfo.ObjectName)
	err = os.WriteFile(objectPath, bts, 0644)
	if err != nil {
		return "", err
	}
	return objectPath, err
}

func (ex *Executor) downloadExecutable(objectId string) (ExecutableConfig, string, error) {
	util.Logger.Infof("try to download executable, objectId=%s", objectId)
	executableZip, err := ex.downloadObject(objectId)
	if err != nil {
		util.Logger.Errorf("download executable failed, err=%s", err.Error())
		return ExecutableConfig{}, "", err
	}
	unzipFile(executableZip, "./")

	configDir, err := findDirectoryWithFile("./", executableConfigFileName)
	if err != nil {
		util.Logger.Errorf("can not find executable config file, err=%s", err.Error())
		return ExecutableConfig{}, "", err
	}
	util.Logger.Infof("find work dir of wasm at %s\n", configDir)

	var executableConfig ExecutableConfig
	executableConfig, err = readExecutableConfig(configDir)
	if err != nil {
		util.Logger.Errorf("can not parse executable config file, err=%s,", err.Error())
	}
	return executableConfig, configDir, err
}

func findDirectoryWithFile(root, targetFile string) (string, error) {
	var result string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Name() == targetFile {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			result = filepath.Dir(abs)
			return filepath.SkipDir
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if result == "" {
		return "", fmt.Errorf("file '%s' not found in '%s'", targetFile, root)
	}

	return result, nil
}

func readExecutableConfig(path string) (ExecutableConfig, error) {
	config := ExecutableConfig{}
	// Open json
	jsonFile, err := os.Open(path + "/" + executableConfigFileName)
	if err != nil {
		return config, err
	}

	// defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(byteValue, &config)
	return config, err
}

func (ex *Executor) downloadInputFiles(objectIds string, config ExecutableConfig) error {
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
			unzipFile(inputPath, ".")
			// check InputDir
			_, err = findDirectoryWithFile(".", config.Data.InputDir)
			if err != nil {
				util.Logger.Errorf("Can not find inputDir err=%s\n", err.Error())
				return err
			}
		}
	}
	return nil
}

func (ex *Executor) uploadFile(dir string, fileName string) (string, error) {
	// read file
	filePath := dir + "/" + fileName
	dataBuf, err := os.ReadFile(filePath)
	if err != nil {
		util.Logger.Error("Can not read input file: " + filePath)
		return "", err
	}

	util.Logger.Infof("---> CreateObject (%s) and HeadObject into bucket (%s) <---\n", fileName, outputBucketName)
	uploadTx, err := ex.Client.CreateObject(context.Background(), outputBucketName, fileName, bytes.NewReader(dataBuf), types.CreateObjectOptions{})
	if err != nil {
		util.Logger.Error("Error create object: " + err.Error())
		return "", err
	}
	_, err = ex.Client.WaitForTx(context.Background(), uploadTx)
	if err != nil {
		util.Logger.Error("Error wait TX: " + err.Error())
		return "", err
	}

	time.Sleep(5 * time.Second)

	util.Logger.Infof("---> PutObject (%s) <---\n", fileName)
	err = ex.Client.PutObject(context.Background(), outputBucketName, fileName, int64(len(dataBuf)),
		bytes.NewReader(dataBuf), types.PutObjectOptions{})
	if err != nil {
		util.Logger.Error("Error put object: " + err.Error())
		return "", err
	}
	time.Sleep(10 * time.Second)
	dataObjectInfo, err := ex.Client.HeadObject(context.Background(), outputBucketName, fileName)
	if err != nil {
		util.Logger.Error("Error HeadObject: " + err.Error())
		return "", err
	}
	util.Logger.Infof("Upload object %s, get ObjectID %s\n", filePath, dataObjectInfo.Id.String())
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
		util.Logger.Error("Fail to update executed task")
		return err
	}

	return err
}
