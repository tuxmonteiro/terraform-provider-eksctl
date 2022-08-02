package cluster

import (
	"fmt"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

func doCheckPodsReadiness(ctx *sdk.Context, cluster *Cluster, id string) error {
	if len(cluster.CheckPodsReadinessConfigs) == 0 {
		return nil
	}

	kubeconfig, err := ioutil.TempFile("", "terraform-provider-eksctl-kubeconfig-")
	if err != nil {
		return err
	}

	kubeconfigPath := kubeconfig.Name()

	if err := kubeconfig.Close(); err != nil {
		return err
	}

	clusterName := cluster.Name + "-" + id

	writeKubeconfigCmd, err := newEksctlCommandWithAWSProfile(cluster, "utils", "write-kubeconfig", "--kubeconfig", kubeconfigPath, "--cluster", clusterName, "--region", cluster.Region)
	if err != nil {
		return fmt.Errorf("creating eksctl-utils-write-kubeconfig command: %w", err)
	}

	if _, err := ctx.Run(writeKubeconfigCmd); err != nil {
		return err
	}

	for _, r := range cluster.CheckPodsReadinessConfigs {
		args := []string{"wait", "--namespace", r.namespace, "--for", "condition=ready", "pod",
			"--timeout", fmt.Sprintf("%ds", r.timeoutSec),
		}

		var matches []string
		for k, v := range r.labels {
			matches = append(matches, k+"="+v)
		}

		args = append(args, "-l", strings.Join(matches, ","))

		var selectorArgs []string

		args = append(args, selectorArgs...)

		kubectlCmd := exec.Command(cluster.KubectlBin, args...)

		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "KUBECONFIG=") {
				kubectlCmd.Env = append(kubectlCmd.Env, env)
			}
		}

		kubectlCmd.Env = append(kubectlCmd.Env, "KUBECONFIG="+kubeconfigPath)

		if _, err := ctx.Run(kubectlCmd); err != nil {
			return err
		}
	}

	return nil
}
