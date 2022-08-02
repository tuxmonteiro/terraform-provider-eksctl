package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
	"log"
	"strconv"
)

type LiveClusterInfo struct {
	KubernetesVersion string
	Revision          int
}

func getLiveClusterInfo(ctx *sdk.Context, d *schema.ResourceData) (*LiveClusterInfo, error) {
	log.Printf("[DEBUG] getting eksctl cluster k8s version with id %q", d.Id())

	m := &Manager{}

	set, err := m.PrepareClusterSet(d)
	if err != nil {
		return nil, err
	}

	cluster := set.Cluster

	clusterName := cluster.Name + "-" + d.Id()

	args := []string{
		"get",
		"cluster",
		"--name", clusterName,
		"--region", cluster.Region,
		"-o", "json",
	}

	cmd, err := newEksctlCommandWithAWSProfile(cluster, args...)
	if err != nil {
		return nil, err
	}

	cmd.Stdin = bytes.NewReader(set.ClusterConfig)

	res, err := ctx.Run(cmd)
	if err != nil {
		return nil, err
	}

	type ClusterData struct {
		Version string            `json:"Version"`
		Tags    map[string]string `json:"Tags"`
	}

	var data []ClusterData

	if err := json.Unmarshal([]byte(res.Output), &data); err != nil {
		return nil, err
	}

	if len(data) != 1 {
		return nil, fmt.Errorf("BUG: expected number of clusters found by running eksctl get cluster: %d\n\n%v", len(data), data)
	}

	var rev int

	{
		revisionTagKey := "tf-provider-eksctl/revision"
		if r, ok := data[0].Tags[revisionTagKey]; ok {
			v, err := strconv.Atoi(r)
			if err != nil {
				return nil, fmt.Errorf("converting tag value for %s to int: %w", revisionTagKey, err)
			}

			rev = v
		}
	}

	return &LiveClusterInfo{
		KubernetesVersion: data[0].Version,
		Revision:          rev,
	}, nil
}
