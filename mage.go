// +build mage

package main

import (
	_ "github.com/cresta/magehelper/cicd/githubactions"
	// mage:import go
	"github.com/cresta/magehelper/gobuild"

	"github.com/cresta/magehelper/docker/registry"
	// mage:import ghcr
	"github.com/cresta/magehelper/docker/registry/ghcr"

	// mage:import lambda
	_ "github.com/cresta/magehelper/lambda"
	// mage:import docker
	_ "github.com/cresta/magehelper/docker"
)

func init() {
	// Install ECR as my registry
	registry.Instance = ghcr.Instance
	gobuild.Instance.BuildMainDirectory = "./cmd/eventbridge-to-slack"
}
