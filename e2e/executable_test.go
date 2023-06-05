package e2e

import (
	"bytes"
	spTypes "github.com/bnb-chain/greenfield/x/sp/types"
	"io"
	"os"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/bnb-chain/greenfield-go-sdk/pkg/utils"
	"github.com/bnb-chain/greenfield-go-sdk/types"
	types2 "github.com/bnb-chain/greenfield/sdk/types"
	storageTestUtil "github.com/bnb-chain/greenfield/testutil/storage"
	permTypes "github.com/bnb-chain/greenfield/x/permission/types"
	storageTypes "github.com/bnb-chain/greenfield/x/storage/types"
	"github.com/stretchr/testify/suite"

	"github.com/bnb-chain/greenfield-execution-provider/e2e/basesuite"
)

type StorageTestSuite1 struct {
	basesuite.BaseSuite
	PrimarySP spTypes.StorageProvider
}

func (s *StorageTestSuite1) SetupSuite() {
	s.BaseSuite.SetupSuite()

	spList, err := s.Client.ListStorageProviders(s.ClientContext, false)
	s.Require().NoError(err)
	for _, sp := range spList {
		if sp.Endpoint != "https://sp0.greenfield.io" {
			s.PrimarySP = sp
		}
	}
}

func TestStorageTestSuite1(t *testing.T) {
	suite.Run(t, new(StorageTestSuite1))
}

func (s *StorageTestSuite1) Test_Executable() {
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

	objectInfo, err := s.Client.HeadObject(s.ClientContext, bucketName, inputName)
	s.Require().NoError(err)
	s.Require().Equal(objectInfo.ObjectName, inputName)
	s.Require().Equal(objectInfo.GetObjectStatus().String(), "OBJECT_STATUS_CREATED")

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
	_, err = s.Client.InvokeExecution(s.ClientContext, objectInfo.Id, []math.Uint{objectInfo.Id}, math.NewUint(10000), "main", []byte("xxx"), types2.TxOption{})
	s.Require().NoError(err)

	time.Sleep(10 * time.Second)

	s.T().Log("---> GetObject <---")
	ior, info, err := s.Client.GetObject(s.ClientContext, bucketName, executableName, types.GetObjectOption{})
	s.Require().NoError(err)
	if err == nil {
		s.Require().Equal(info.ObjectName, executableName)
		objectBytes, err := io.ReadAll(ior)
		s.Require().NoError(err)
		s.Require().Equal(objectBytes, executableBuf)
	}

	s.T().Log("---> PutObjectPolicy <---")
	principal, _, err := types.NewAccount("principal")
	s.Require().NoError(err)
	principalWithAccount, err := utils.NewPrincipalWithAccount(principal.GetAddress())
	s.Require().NoError(err)
	statements := []*permTypes.Statement{
		{
			Effect: permTypes.EFFECT_ALLOW,
			Actions: []permTypes.ActionType{
				permTypes.ACTION_GET_OBJECT,
			},
			Resources:      nil,
			ExpirationTime: nil,
			LimitSize:      nil,
		},
	}
	policy, err := s.Client.PutObjectPolicy(s.ClientContext, bucketName, executableName, principalWithAccount, statements, types.PutPolicyOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, policy)
	s.Require().NoError(err)

	s.T().Log("--->  GetObjectPolicy <---")
	objectPolicy, err := s.Client.GetObjectPolicy(s.ClientContext, bucketName, executableName, principal.GetAddress().String())
	s.Require().NoError(err)
	s.T().Logf("get object policy:%s\n", objectPolicy.String())

	s.T().Log("---> DeleteObjectPolicy (input) <---")
	deleteInputObjectPolicy, err := s.Client.DeleteObjectPolicy(s.ClientContext, bucketName, inputName, principal.GetAddress().String(), types.DeletePolicyOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteInputObjectPolicy)
	s.Require().NoError(err)

	s.T().Log("---> DeleteObjectPolicy (executable) <---")
	deleteObjectPolicy, err := s.Client.DeleteObjectPolicy(s.ClientContext, bucketName, executableName, principal.GetAddress().String(), types.DeletePolicyOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteObjectPolicy)
	s.Require().NoError(err)

	s.T().Log("---> DeleteObject (input) <---")
	deleteInputObject, err := s.Client.DeleteObject(s.ClientContext, bucketName, inputName, types.DeleteObjectOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteInputObject)
	s.Require().NoError(err)
	_, err = s.Client.HeadObject(s.ClientContext, bucketName, inputName)
	s.Require().Error(err)

	s.T().Log("---> DeleteObject (executable) <---")
	deleteObject, err := s.Client.DeleteObject(s.ClientContext, bucketName, executableName, types.DeleteObjectOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteObject)
	s.Require().NoError(err)
	_, err = s.Client.HeadObject(s.ClientContext, bucketName, executableName)
	s.Require().Error(err)

}
