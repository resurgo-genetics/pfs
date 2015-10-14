package persist

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.pedge.io/google-protobuf"
	"golang.org/x/net/context"

	"github.com/satori/go.uuid"
	"go.pachyderm.com/pachyderm/src/pkg/require"
)

func TestBasicRethink(t *testing.T) {
	runTestRethink(t, testBasicRethink)
}

func testBasicRethink(t *testing.T, apiServer APIServer) {
	job, err := apiServer.CreateJob(
		context.Background(),
		&Job{
			Spec: &Job_PipelineId{
				PipelineId: "456",
			},
		},
	)
	require.NoError(t, err)
	getJob, err := apiServer.GetJobByID(
		context.Background(),
		&google_protobuf.StringValue{
			Value: job.Id,
		},
	)
	require.NoError(t, err)
	require.Equal(t, job.Id, getJob.Id)
	require.Equal(t, "456", getJob.GetPipelineId())
}

func runTestRethink(t *testing.T, testFunc func(*testing.T, APIServer)) {
	apiServer, err := getTestRethinkAPIServer()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, apiServer.Close())
	}()
	testFunc(t, newLogAPIServer(apiServer))
}

func getTestRethinkAPIServer() (*rethinkAPIServer, error) {
	address, err := getTestRethinkAddress()
	if err != nil {
		return nil, err
	}
	databaseName := strings.Replace(uuid.NewV4().String(), "-", "", -1)
	if err := InitDBs(address, databaseName); err != nil {
		return nil, err
	}
	return newRethinkAPIServer(address, databaseName)
}

func getTestRethinkAddress() (string, error) {
	rethinkAddr := os.Getenv("RETHINK_PORT_28015_TCP_ADDR")
	if rethinkAddr == "" {
		return "", errors.New("RETHINK_PORT_28015_TCP_ADDR not set")
	}
	return fmt.Sprintf("%s:28015", rethinkAddr), nil
}
