package e2e

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/bnb-chain/greenfield-execution-provider/e2e/basesuite"
	"github.com/bnb-chain/greenfield-go-sdk/pkg/utils"
	"github.com/bnb-chain/greenfield-go-sdk/types"
	types2 "github.com/bnb-chain/greenfield/sdk/types"
	storageTestUtil "github.com/bnb-chain/greenfield/testutil/storage"
	permTypes "github.com/bnb-chain/greenfield/x/permission/types"
	spTypes "github.com/bnb-chain/greenfield/x/sp/types"
	storageTypes "github.com/bnb-chain/greenfield/x/storage/types"
	"github.com/stretchr/testify/suite"
)

type StorageTestSuite struct {
	basesuite.BaseSuite
	PrimarySP spTypes.StorageProvider
}

func (s *StorageTestSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()

	spList, err := s.Client.ListStorageProviders(s.ClientContext, false)
	s.Require().NoError(err)
	for _, sp := range spList {
		if sp.Endpoint != "https://sp0.greenfield.io" {
			s.PrimarySP = sp
		}
	}
}

func TestStorageTestSuite(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}

func (s *StorageTestSuite) Test_Object() {
	bucketName := storageTestUtil.GenRandomBucketName()
	objectName := storageTestUtil.GenRandomObjectName()

	bucketTx, err := s.Client.CreateBucket(s.ClientContext, bucketName, s.PrimarySP.OperatorAddress, types.CreateBucketOptions{})
	s.Require().NoError(err)

	_, err = s.Client.WaitForTx(s.ClientContext, bucketTx)
	s.Require().NoError(err)

	bucketInfo, err := s.Client.HeadBucket(s.ClientContext, bucketName)
	s.Require().NoError(err)
	if err == nil {
		s.Require().Equal(bucketInfo.Visibility, storageTypes.VISIBILITY_TYPE_PRIVATE)
	}

	var buffer bytes.Buffer
	line := `1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890`
	// Create 1MiB content where each line contains 1024 characters.
	for i := 0; i < 1024*100; i++ {
		buffer.WriteString(fmt.Sprintf("[%05d] %s\n", i, line))
	}

	s.T().Log("---> CreateObject and HeadObject <---")
	objectTx, err := s.Client.CreateObject(s.ClientContext, bucketName, objectName, bytes.NewReader(buffer.Bytes()), types.CreateObjectOptions{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, objectTx)
	s.Require().NoError(err)

	time.Sleep(5 * time.Second)
	objectInfo, err := s.Client.HeadObject(s.ClientContext, bucketName, objectName)
	s.Require().NoError(err)
	s.Require().Equal(objectInfo.ObjectName, objectName)
	s.Require().Equal(objectInfo.GetObjectStatus().String(), "OBJECT_STATUS_CREATED")

	s.T().Log("---> invoke execution <---")
	_, err = s.Client.InvokeExecution(s.ClientContext, objectInfo.Id, []math.Uint{objectInfo.Id}, math.NewUint(100), "main", []byte("xxx"), types2.TxOption{})
	s.Require().NoError(err)

	s.T().Log("---> PutObject and GetObject <---")
	err = s.Client.PutObject(s.ClientContext, bucketName, objectName, int64(buffer.Len()),
		bytes.NewReader(buffer.Bytes()), types.PutObjectOptions{})
	s.Require().NoError(err)

	time.Sleep(10 * time.Second)
	objectInfo, err = s.Client.HeadObject(s.ClientContext, bucketName, objectName)
	s.Require().NoError(err)
	if err == nil {
		s.Require().Equal(objectInfo.GetObjectStatus().String(), "OBJECT_STATUS_SEALED")
	}

	ior, info, err := s.Client.GetObject(s.ClientContext, bucketName, objectName, types.GetObjectOption{})
	s.Require().NoError(err)
	if err == nil {
		s.Require().Equal(info.ObjectName, objectName)
		objectBytes, err := io.ReadAll(ior)
		s.Require().NoError(err)
		s.Require().Equal(objectBytes, buffer.Bytes())
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
	policy, err := s.Client.PutObjectPolicy(s.ClientContext, bucketName, objectName, principalWithAccount, statements, types.PutPolicyOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, policy)
	s.Require().NoError(err)

	s.T().Log("--->  GetObjectPolicy <---")
	objectPolicy, err := s.Client.GetObjectPolicy(s.ClientContext, bucketName, objectName, principal.GetAddress().String())
	s.Require().NoError(err)
	s.T().Logf("get object policy:%s\n", objectPolicy.String())

	s.T().Log("---> DeleteObjectPolicy <---")
	deleteObjectPolicy, err := s.Client.DeleteObjectPolicy(s.ClientContext, bucketName, objectName, principal.GetAddress().String(), types.DeletePolicyOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteObjectPolicy)
	s.Require().NoError(err)

	s.T().Log("---> DeleteObject <---")
	deleteObject, err := s.Client.DeleteObject(s.ClientContext, bucketName, objectName, types.DeleteObjectOption{})
	s.Require().NoError(err)
	_, err = s.Client.WaitForTx(s.ClientContext, deleteObject)
	s.Require().NoError(err)
	_, err = s.Client.HeadObject(s.ClientContext, bucketName, objectName)
	s.Require().Error(err)
}
