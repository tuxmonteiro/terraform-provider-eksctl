package main

import (
	"github.com/hashicorp/terraform-plugin-sdk/plugin"
	"github.com/tuxmonteiro/terraform-provider-eksctl/pkg/provider"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: provider.Provider})
}
