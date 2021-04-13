package main

import (
	"context"
	"encoding/json"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
	"testing"
)

type dummyClient struct {
	ctx  context.Context
	cid  string
	opts []slack.MsgOption
}

func (d *dummyClient) AuthTestContext(ctx context.Context) (response *slack.AuthTestResponse, err error) {
	return &slack.AuthTestResponse{}, nil
}

func (d *dummyClient) SendMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (_channel string, _timestamp string, _text string, err error) {
	d.ctx = ctx
	d.cid = channelID
	d.opts = options
	return "", "", "", nil
}

const sampleEvent = `{
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

const sampleEvent2 = `{
    "version": "0",
    "id": "85fc3613-e913-7fc4-a80c-a3753e4aa9ae",
    "detail-type": "ECR Image Scan",
    "source": "aws.ecr",
    "account": "123456789012",
    "time": "2019-10-29T02:36:48Z",
    "region": "us-east-1",
    "resources": [
        "arn:aws:ecr:us-east-1:123456789012:repository/my-repo"
    ],
    "detail": {
        "scan-status": "COMPLETE",
        "repository-name": "my-repo",
        "finding-severity-counts": {
	       "CRITICAL": 10,
	       "MEDIUM": 9
	     },
        "image-digest": "sha256:7f5b2640fe6fb4f46592dfd3410c4a79dac4f89e4782432e0378abcd1234",
        "image-tags": []
    }
}`

func TestService_Main(t *testing.T) {
	checkTemplate := func(slackMsg string, filterTemplate string, filterRegex string, eventMsg string, expected string) func(t *testing.T) {
		return func(t *testing.T) {
			var handlerFunc func(ctx context.Context, input map[string]interface{}) error
			var osExit int
			cc := &dummyClient{}
			s := Service{
				osExit: func(i int) {
					osExit = i
				},
				config: config{
					LogLevel:       "warn",
					MsgToSend:      slackMsg,
					FilterTemplate: filterTemplate,
					FilterRegex:    filterRegex,
				}.WithDefaults(),
				SlackConstructor: func(_ string, _ ...slack.Option) SlackClient {
					return cc
				},
				LambdaStart: func(handler interface{}) {
					handlerFunc = handler.(func(ctx context.Context, input map[string]interface{}) error)
				},
			}
			s.Main()
			require.Equal(t, 0, osExit)
			var into map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(eventMsg), &into))
			require.NoError(t, handlerFunc(context.Background(), into))
			require.Equal(t, expected, getText(cc.ctx))
			t.Log(osExit)
		}
	}
	t.Run("verify_basic", checkTemplate("hello world", "", "", sampleEvent, "hello world"))
	t.Run("verify_template", checkTemplate(`region {{index . "region"}}`, "", "", sampleEvent, "region us-east-1"))
	t.Run("verify_inner_template", checkTemplate(`status {{index . "detail" "scan-status"}}`, "", "", sampleEvent2, "status COMPLETE"))
	t.Run("check_sprig", checkTemplate(`res {{index . "resources" | join "," }}`, "", "", sampleEvent2, "res arn:aws:ecr:us-east-1:123456789012:repository/my-repo"))
	t.Run("check_sprig_default", checkTemplate(`missing value {{index . "blarg" | default "unset"}}`, "", "", sampleEvent2, "missing value unset"))

	t.Run("basic_filter", checkTemplate(`never`, `{{if index . "blarg"}}hi{{end}}`, "", sampleEvent, ""))

	t.Run("filter_key_exists", checkTemplate(`key_exists`, `{{if index . "account"}}.{{end}}`, "", sampleEvent, "key_exists"))
	t.Run("filter_key_not_exists", checkTemplate(`key_exists`, `{{if index . "account2"}}.{{end}}`, "", sampleEvent, ""))

	t.Run("filter_key_equals", checkTemplate(`key_equals`, `{{index . "account"}}`, "123456789012", sampleEvent, "key_equals"))
	t.Run("filter_key_not_equals", checkTemplate(`key_not_equals`, `{{index . "account"}}`, "123456789013", sampleEvent, ""))
}
