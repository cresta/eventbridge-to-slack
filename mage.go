// +build mage

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	_ "github.com/cresta/magehelper/cicd/githubactions"
	"github.com/cresta/magehelper/env"

	// mage:import go
	_ "github.com/cresta/magehelper/gobuild"

	"github.com/cresta/magehelper/docker/registry"
	// mage:import ghcr
	"github.com/cresta/magehelper/docker/registry/ghcr"

	// mage:import lambda
	_ "github.com/cresta/magehelper/lambda"
	// mage:import docker
	_ "github.com/cresta/magehelper/docker"
)

func init() {
	// Install github as my registry
	registry.Instance = ghcr.Instance
	env.Default("DOCKER_MUTABLE_TAGS", "true")
}

func SampleEvent(ctx context.Context) error {
	event := `{
  "account": "123456789012",
  "detail": {
    "action-type": "PUSH",
    "image-digest": "sha256:f98d67af8e53a536502bfc600de3266556b06ed635a32d60aa7a5fe6d7e609d7",
    "image-tag": "latest",
    "repository-name": "ubuntu",
    "result": "SUCCESS"
  },
  "detail-type": "ECR Image Action",
  "id": "4f5ec4d5-4de4-7aad-a046-56d5cfe1df0e",
  "region": "us-east-1",
  "resources": [],
  "source": "aws.ecr",
  "time": "2019-08-06T00:58:09Z",
  "version": "0"
}`
	resp, err := http.Post("http://localhost:9000/2015-03-31/functions/function/invocations", "applicatoin/json", strings.NewReader(event))
	if err != nil {
		return err
	}
	res, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	fmt.Println(string(res))
	return nil
}
