package provider

import (
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/resource/cluster"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/resource/courier"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/resource/iamserviceaccount"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/resource/nodegroup"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk/tfsdk"
)

// Provider returns a terraform.ResourceProvider.
func Provider() terraform.ResourceProvider {

	// The actual provider
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			tfsdk.KeyAssumeRole: tfsdk.SchemaAssumeRole(),
		},
		ResourcesMap: map[string]*schema.Resource{
			"eksctl_cluster":                cluster.ResourceCluster(),
			"eksctl_nodegroup":              nodegroup.Resource(),
			"eksctl_iamserviceaccount":      iamserviceaccount.Resource(),
			"eksctl_courier_alb":            courier.ResourceALB(),
			"eksctl_courier_route53_record": courier.ResourceRoute53Record(),
		},
		ConfigureFunc: providerConfigure(),
	}
}
