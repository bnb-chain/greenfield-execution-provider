package e2e

import (
	"bytes"
	"os"
	"time"

	"cosmossdk.io/math"
	"github.com/bnb-chain/greenfield-go-sdk/types"
	types2 "github.com/bnb-chain/greenfield/sdk/types"
	storageTestUtil "github.com/bnb-chain/greenfield/testutil/storage"
	storageTypes "github.com/bnb-chain/greenfield/x/storage/types"
)

func (s *StorageTestSuite) Test_Executable() {
	bucketName := storageTestUtil.GenRandomBucketName()
	inputName := "input.zip"
	executableName := "wordcount.zip"

	s.T().Log("---> Create Bucket <---")

	bucketTx, err := s.Client.CreateBucket(s.ClientContext, bucketName, s.PrimarySP.OperatorAddress, types.CreateBucketOptions{})
	s.Require().NoError(err)

	s.T().Log("---> Create Bucket tx <---", "tx", bucketTx)

	_, err = s.Client.WaitForTx(s.ClientContext, bucketTx)
	s.Require().NoError(err)

	bucketInfo, err := s.Client.HeadBucket(s.ClientContext, bucketName)
	s.Require().NoError(err)
	if err == nil {
		s.Require().Equal(bucketInfo.Visibility, storageTypes.VISIBILITY_TYPE_PRIVATE)
	}

	// read file
	inputBuf, err := os.ReadFile(inputName)
	if err != nil {
		s.T().Log("Can not read input file")
		return
	}

	s.T().Log("---> CreateObject (input) and HeadObject <---")
	inputTx, err := s.Client.CreateObject(s.ClientContext, bucketName, inputName, bytes.NewReader(inputBuf), types.CreateObjectOptions{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, inputTx)
	s.Require().NoError(err)

	time.Sleep(5 * time.Second)

	s.T().Log("---> PutObject (input) <---")
	err = s.Client.PutObject(s.ClientContext, bucketName, inputName, int64(len(inputBuf)),
		bytes.NewReader(inputBuf), types.PutObjectOptions{})
	s.Require().NoError(err)

	inputObjectInfo, err := s.Client.HeadObject(s.ClientContext, bucketName, inputName)
	s.Require().NoError(err)
	s.Require().Equal(inputObjectInfo.ObjectName, inputName)
	s.Require().Equal(inputObjectInfo.GetObjectStatus().String(), "OBJECT_STATUS_CREATED")

	executableBuf, err := os.ReadFile(executableName)
	if err != nil {
		s.T().Log("Can not read executable file")
		return
	}

	s.T().Log("---> CreateObject (executable) and HeadObject <---")
	executableTx, err := s.Client.CreateObject(s.ClientContext, bucketName, executableName, bytes.NewReader(executableBuf), types.CreateObjectOptions{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, executableTx)
	s.Require().NoError(err)

	time.Sleep(5 * time.Second)

	executableInfo, err := s.Client.HeadObject(s.ClientContext, bucketName, executableName)
	s.Require().NoError(err)
	s.Require().Equal(executableInfo.ObjectName, executableName)
	s.Require().Equal(executableInfo.GetObjectStatus().String(), "OBJECT_STATUS_CREATED")

	s.T().Log("---> PutObject (executable) <---")
	err = s.Client.PutObject(s.ClientContext, bucketName, executableName, int64(len(executableBuf)),
		bytes.NewReader(executableBuf), types.PutObjectOptions{})
	s.Require().NoError(err)

	time.Sleep(10 * time.Second)

	executableInfo, err = s.Client.HeadObject(s.ClientContext, bucketName, executableName)
	s.Require().NoError(err)
	s.Require().Equal(executableInfo.ObjectName, executableName)
	s.Require().Equal(executableInfo.GetObjectStatus().String(), "OBJECT_STATUS_SEALED")

	s.T().Log("---> invoke execution <---")
	_, err = s.Client.InvokeExecution(s.ClientContext, executableInfo.Id, []math.Uint{inputObjectInfo.Id}, math.NewUint(10000), "main", []byte("xxx"), types2.TxOption{})
	s.Require().NoError(err)
}
