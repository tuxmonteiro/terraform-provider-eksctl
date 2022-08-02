package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk/api"
	"log"
	"time"

	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/courier"
	"github.com/rs/xid"
	"gopkg.in/yaml.v3"
)

const KeyName = "name"
const KeyRegion = "region"
const KeyProfile = "profile"
const KeyAPIVersion = "api_version"
const KeyVersion = "version"
const KeyTags = "tags"
const KeyRevision = "revision"
const KeySpec = "spec"
const KeyBin = "eksctl_bin"
const KeyEksctlVersion = "eksctl_version"
const KeyKubeconfigPath = "kubeconfig_path"
const KeyKubectlBin = "kubectl_bin"
const KeyPodsReadinessCheck = "pods_readiness_check"
const KeyKubernetesResourceDeletionBeforeDestroy = "kubernetes_resource_deletion_before_destroy"
const KeyALBAttachment = "alb_attachment"
const KeyVPCID = "vpc_id"
const KeyManifests = "manifests"
const KeyMetrics = "metrics"
const KeyDrainNodeGroups = "drain_node_groups"
const KeyIAMIdentityMapping = "iam_identity_mapping"
const KeyAWSAuthConfigMap = "aws_auth_configmap"
const (
	KeyTargetGroupARNs  = "target_group_arns"
	KeyOIDCProviderURL  = "oidc_provider_url"
	KeyOIDCProviderARN  = "oidc_provider_arn"
	KeySecurityGroupIDs = "security_group_ids"
)

const DefaultAPIVersion = "eksctl.io/v1alpha5"
const DefaultVersion = "1.16"

var ValidDeleteK8sResourceKinds = []string{"deployment", "deploy", "pod", "service", "svc", "statefulset", "job"}

type CheckPodsReadiness struct {
	namespace  string
	labels     map[string]string
	timeoutSec int
}

func newClusterID() string {
	return xid.New().String()
}

type Cluster struct {
	EksctlBin  string
	KubectlBin string
	Name       string
	Region     string
	Profile    string
	APIVersion string
	Version    string
	VPCID      string
	Spec       string
	Output     string
	Manifests  []string

	// EksctlVersion lets the provider to install the eksctl binary for the specified versino using shoal
	EksctlVersion string

	CheckPodsReadinessConfigs []CheckPodsReadiness

	DeleteKubernetesResourcesBeforeDestroy []DeleteKubernetesResource

	PublicSubnetIDs  []string
	PrivateSubnetIDs []string
	ALBAttachments   []courier.ALBAttachment
	TargetGroupARNs  []string
	Metrics          []courier.Metric
	AssumeRoleConfig *sdk.AssumeRoleConfig
}

func (c Cluster) IAMWithOIDCEnabled() (bool, error) {
	var config EksctlClusterConfig

	if err := yaml.Unmarshal([]byte(c.Spec), &config); err != nil {
		return false, fmt.Errorf("parsing cluster.yaml: %w\nCONTENT:\n%s", err, c.Spec)
	}

	return config.IAM.WithOIDC, nil
}

func (c Cluster) GitOpsEnabled() (bool, error) {
	var config EksctlClusterConfig

	if err := yaml.Unmarshal([]byte(c.Spec), &config); err != nil {
		return false, fmt.Errorf("parsing cluster.yaml: %w\nCONTENT:\n%s", err, c.Spec)
	}

	if config.Git != nil {
		if r, ok := config.Git["repo"].(map[string]interface{}); ok {
			return len(r) > 0, nil
		}
	}

	return false, nil
}

type DeleteKubernetesResource struct {
	Namespace string
	Name      string
	Kind      string
}

type EksctlClusterConfig struct {
	VPC        VPC         `yaml:"vpc"`
	NodeGroups []NodeGroup `yaml:"nodeGroups"`

	// omitempty is required from eksctl-perspective. Otherwise, eksctl fails due to `git.repo.url is required` error
	// on `git: {}` in the generated cluster.yaml.
	Git  map[string]interface{} `yaml:"git,omitempty"`
	Rest map[string]interface{} `yaml:",inline"`
	IAM  IAM                    `yaml:"iam"`
}

type IAM struct {
	WithOIDC bool                   `yaml:"withOIDC"`
	Rest     map[string]interface{} `yaml:",inline"`
}

type VPC struct {
	ID      string                 `yaml:"id"`
	Subnets Subnets                `yaml:"subnets"`
	Rest    map[string]interface{} `yaml:",inline"`
}

type Subnets struct {
	Public  map[string]Subnet `yaml:"public"`
	Private map[string]Subnet `yaml:"private"`
}

type Subnet struct {
	ID string `yaml:"id"`
}

type ClusterSet struct {
	ClusterID        string
	ClusterName      ClusterName
	Cluster          *Cluster
	ClusterConfig    []byte
	ListenerStatuses ListenerStatuses
	CanaryOpts       courier.CanaryOpts
}

type NodeGroup struct {
	Name            string                 `yaml:"name"`
	TargetGroupARNS []string               `yaml:"targetGroupARNS"`
	Rest            map[string]interface{} `yaml:",inline"`
}

type Manager struct {
	DisableClusterNameSuffix bool
}

func (m *Manager) PrepareClusterSet(d api.UniqueResourceGetter, optNewId ...string) (*ClusterSet, error) {
	a, err := ReadCluster(d)
	if err != nil {
		return nil, err
	}

	spec := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(a.Spec), spec); err != nil {
		return nil, fmt.Errorf("parsing used-provided cluster.yaml: %w: INPUT:\n%s", err, a.Spec)
	}

	if a.VPCID != "" {
		var set bool

		if _, ok := spec["vpc"]; !ok {
			spec["vpc"] = map[string]interface{}{}
		}

		rawVPC := spec["vpc"]
		switch vpc := rawVPC.(type) {
		case map[interface{}]interface{}:
			vpc["id"] = a.VPCID
			set = true
		case map[string]interface{}:
			vpc["id"] = a.VPCID
			set = true
		}

		if !set {
			return nil, fmt.Errorf("bug: failed to set vpc.id in cluster.yaml: type = %T, value = %v", rawVPC, rawVPC)
		}
	}

	var specStr string
	{
		var buf bytes.Buffer

		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)

		if err := enc.Encode(spec); err != nil {
			return nil, err
		}

		specStr = buf.String()
	}

	var id string
	var newId string

	if len(optNewId) > 0 {
		id = optNewId[0]
		newId = optNewId[0]
	} else {
		id = d.Id()
	}

	if id == "" {
		return nil, errors.New("Missing Resource ID. This must be a bug!")
	}

	clusterName := m.getClusterName(a, id)

	listenerStatuses, err := planListenerChanges(a, d.Id(), newId)
	if err != nil {
		return nil, fmt.Errorf("planning listener changes: %v", err)
	}

	tagsJson := "{}"
	if v := d.Get(KeyTags); v != nil {
		if tags, ok := v.(map[string]interface{}); ok {
			tagsJsonBs, err := json.Marshal(tags)
			if err != nil {
				return nil, fmt.Errorf("marshalling eksctl_cluster tags to json: %w", err)
			}

			tagsJson = string(tagsJsonBs)
		}
	}

	seedClusterConfig := []byte(fmt.Sprintf(`
apiVersion: %s
kind: ClusterConfig

metadata:
  name: %q
  region: %q
  version: %q
  tags: %s

%s
`, a.APIVersion, clusterName, a.Region, a.Version, tagsJson, specStr))

	c := clusterConfigNew()

	if err := yaml.Unmarshal(seedClusterConfig, &c); err != nil {
		return nil, fmt.Errorf("parsing generate cluster.yaml: %w: INPUT:\n%s", err, string(seedClusterConfig))
	}
	//
	//for i := range c.NodeGroups {
	//	ng := c.NodeGroups[i]
	//
	//	for _, l := range listenerStatuses {
	//		for _, a := range l.ALBAttachments {
	//			if ng.Name == a.NodeGroupName {
	//				ng.TargetGroupARNS = append(ng.TargetGroupARNS, *l.DesiredTG.TargetGroupArn)
	//			}
	//		}
	//	}
	//}

	mergedClusterConfig, err := clusterConfigToYAML(c)
	if err != nil {
		return nil, err
	}

	log.Printf("seed cluster config:\n%s", string(seedClusterConfig))
	log.Printf("merged cluster config:\n%s", string(mergedClusterConfig))

	for _, s := range c.VPC.Subnets.Public {
		a.PublicSubnetIDs = append(a.PublicSubnetIDs, s.ID)
	}

	for _, s := range c.VPC.Subnets.Public {
		a.PrivateSubnetIDs = append(a.PrivateSubnetIDs, s.ID)
	}

	a.VPCID = c.VPC.ID

	return &ClusterSet{
		ClusterID:        id,
		ClusterName:      clusterName,
		Cluster:          a,
		ClusterConfig:    mergedClusterConfig,
		ListenerStatuses: listenerStatuses,
		CanaryOpts: courier.CanaryOpts{
			CanaryAdvancementInterval: 5 * time.Second,
			CanaryAdvancementStep:     5,
			Region:                    a.Region,
			ClusterName:               string(clusterName),
		},
	}, nil
}

func clusterConfigToYAML(c EksctlClusterConfig) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(c); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func clusterConfigNew() EksctlClusterConfig {
	return EksctlClusterConfig{
		VPC: VPC{
			ID: "",
			Subnets: Subnets{
				Public:  map[string]Subnet{},
				Private: map[string]Subnet{},
			},
			Rest: map[string]interface{}{},
		},
		NodeGroups: []NodeGroup{},
		Rest:       map[string]interface{}{},
	}
}

type ClusterName string

func (m *Manager) getClusterName(cluster *Cluster, id string) ClusterName {
	if m.DisableClusterNameSuffix {
		return ClusterName(cluster.Name)
	}
	return ClusterName(fmt.Sprintf("%s-%s", cluster.Name, id))
}
