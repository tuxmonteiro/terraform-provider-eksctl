terraform {
  required_providers {
    eksctl = {
      source = "mumoshu/eksctl"
      # source = "example.com/mumoshu/eksctl"
      version = "0.16.2"
    }
  }
}

provider "eksctl" {
  # Configuration options
}
# provider "eksctl" {}


# terraform {
#   required_providers {
#     aws = {
#       source = "hashicorp/aws"
#       version = "4.14.0"
#     }
#   }
# }

# provider "aws" {
#   # Configuration options
# }



resource "eksctl_cluster" "primary" {
  name = "primary"
  region = "us-east-2"
  spec = <<EOS

nodeGroups:
  - name: ng2
    instanceType: m5.large
    desiredCapacity: 1
EOS
}
