package cluster

import (
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
)

func AWSSessionFromCluster(cluster *Cluster) *session.Session {
	sess, _ := sdk.AWSCredsFromValues(cluster.Region, cluster.Profile, cluster.AssumeRoleConfig)

	return sess
}
