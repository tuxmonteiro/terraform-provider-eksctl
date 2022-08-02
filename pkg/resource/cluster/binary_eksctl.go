package cluster

import (
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
)

func prepareEksctlBinary(cluster *Cluster) (*string, error) {
	return sdk.PrepareExecutable(cluster.EksctlBin, "eksctl", cluster.EksctlVersion)
}
