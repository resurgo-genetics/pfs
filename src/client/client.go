package client

import (
	"fmt"
	"os"

	"go.pedge.io/proto/version"
	"google.golang.org/grpc"

	"github.com/pachyderm/pachyderm/src/client/pfs"
	"github.com/pachyderm/pachyderm/src/client/pps"
)

const (
	// MajorVersion is the current major version for pachyderm.
	MajorVersion = 1
	// MinorVersion is the current minor version for pachyderm.
	MinorVersion = 0
	// MicroVersion is the patch number for pachyderm.
	MicroVersion = 0
)

var (
	// Version is the current version for pachyderm.
	Version = &protoversion.Version{
		Major:      MajorVersion,
		Minor:      MinorVersion,
		Micro:      MicroVersion,
		Additional: getBuildNumber(),
	}
)

type PfsAPIClient pfs.APIClient
type PpsAPIClient pps.APIClient

type APIClient struct {
	PfsAPIClient
	PpsAPIClient
}

// NewFromAddress constructs a new APIClient which the server at pachAddr.
func NewFromAddress(pachAddr string) (*APIClient, error) {
	clientConn, err := grpc.Dial(pachAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return &APIClient{
		pfs.NewAPIClient(clientConn),
		pps.NewAPIClient(clientConn),
	}, nil
}

// NewInCluster constructs a new APIClient using env vars that Kubernetes creates.
// This should be used to access Pachyderm from within a Kubernetes cluster
// with Pachyderm running on it.
func NewInCluster() (*APIClient, error) {
	pachAddr := os.Getenv("PACHD_PORT_650_TCP_ADDR")

	if pachAddr == "" {
		return nil, fmt.Errorf("PACHD_PORT_650_TCP_ADDR not set")
	}

	return NewFromAddress(fmt.Sprintf("%v:650", pachAddr))
}

func PrettyPrintVersion(version *protoversion.Version) string {
	result := fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Micro)
	if version.Additional != "" {
		result += fmt.Sprintf("(%s)", version.Additional)
	}
	return result
}

func getBuildNumber() string {
	value := os.Getenv("PACH_BUILD_NUMBER")
	if value == "" {
		value = "dirty"
	}
	return value
}
