package cluster

import (
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
)

func mustNewContext(cluster *Cluster) *sdk.Context {
	sess, creds := sdk.AWSCredsFromValues(cluster.Region, cluster.Profile, cluster.AssumeRoleConfig)

	return &sdk.Context{Sess: sess, Creds: creds}
}
