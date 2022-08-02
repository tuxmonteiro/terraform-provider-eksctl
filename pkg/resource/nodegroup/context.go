package nodegroup

import (
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk/tfsdk"
)

func mustContext(a *schema.ResourceData) *sdk.Context {
	config := tfsdk.ConfigFromResourceData(a)
	sess, creds := sdk.AWSCredsFromConfig(config)

	return &sdk.Context{Sess: sess, Creds: creds}
}
