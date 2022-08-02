package tfsdk

import (
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/sdk/api"
)

func ConfigFromResourceData(d api.Getter, opts ...SchemaOption) *sdk.Config {
	region, profile := GetAWSRegionAndProfile(d, opts...)

	assumeRoleConfig := GetAssumeRoleConfig(d, opts...)

	return &sdk.Config{
		Region:     region,
		Profile:    profile,
		AssumeRole: assumeRoleConfig,
	}
}
