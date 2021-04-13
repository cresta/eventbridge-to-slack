package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/cresta/zapctx"
	"github.com/slack-go/slack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func setupLogging(logLevel string) (*zapctx.Logger, error) {
	zapCfg := zap.NewProductionConfig()
	var lvl zapcore.Level
	logLevelErr := lvl.UnmarshalText([]byte(logLevel))
	if logLevelErr == nil {
		zapCfg.Level.SetLevel(lvl)
	}
	l, err := zapCfg.Build(zap.AddCaller())
	if err != nil {
		return nil, err
	}
	retLogger := zapctx.New(l)
	retLogger.IfErr(logLevelErr).Warn(context.Background(), "unable to parse log level")
	return retLogger, nil
}

type config struct {
	LogLevel          string
	SlackClientSecret string
	FilterRegex       string
	FilterTemplate    string
	MsgToSend         string
	SlackChannel      string
}

func (c config) WithDefaults() config {
	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
	if c.FilterRegex == "" {
		c.FilterRegex = ".+"
	}
	if c.MsgToSend == "" {
		c.MsgToSend = "Event seen"
	}
	return c
}

func (c config) filterPasswords() config {
	c.SlackClientSecret = fmt.Sprintf("<hidden len=%d>", len(c.SlackClientSecret))
	return c
}

func getConfig() config {
	return config{
		LogLevel:          os.Getenv("LOG_LEVEL"),
		SlackClientSecret: os.Getenv("SLACK_CLIENT_SECRET"),
		SlackChannel:      os.Getenv("SLACK_CHANNEL"),
		FilterTemplate:    os.Getenv("FILTER_TEMPLATE"),
		FilterRegex:       os.Getenv("FILTER_REGEX"),
		MsgToSend:         os.Getenv("MSG_TO_SEND"),
	}.WithDefaults()
}

type Service struct {
	osExit func(int)
	config config
	log    *zapctx.Logger
	server *Server

	SlackConstructor SlackConstructor
	LambdaStart      LambdaStart
}

var instance = Service{
	osExit: os.Exit,
	config: getConfig(),
	SlackConstructor: func(token string, options ...slack.Option) SlackClient {
		return slack.New(token, options...)
	},
	LambdaStart: lambda.Start,
}

type SlackConstructor func(token string, options ...slack.Option) SlackClient

type LambdaStart func(handler interface{})

var _ LambdaStart = lambda.Start

func main() {
	instance.Main()
}

func (m *Service) Main() {
	cfg := m.config
	if m.log == nil {
		var err error
		m.log, err = setupLogging(m.config.LogLevel)
		if err != nil {
			fmt.Printf("Unable to setup logging for service: %v", err)
			m.osExit(1)
			return
		}
	}
	m.log.Info(context.Background(), "Starting", zap.Any("config", m.config.filterPasswords()))

	m.log = m.log.DynamicFields()

	ctx := context.Background()
	var err error
	m.server, err = m.setupServer(ctx, cfg, m.log)
	if err != nil {
		m.log.IfErr(err).Error(ctx, "unable to setup server")
		m.osExit(1)
		return
	}
	m.LambdaStart(m.server.handleRequest)
}

func (m *Service) setupServer(ctx context.Context, cfg config, log *zapctx.Logger) (*Server, error) {
	var er = &EventRule{}
	var err error
	er.MsgToSend, err = template.New("base").Funcs(sprig.TxtFuncMap()).Parse(cfg.MsgToSend)
	if err != nil {
		return nil, fmt.Errorf("unable to compile slack msg template: %w", err)
	}
	if cfg.FilterTemplate != "" {
		er.FilterTemplate, err = template.New("base").Funcs(sprig.TxtFuncMap()).Parse(cfg.FilterTemplate)
		if err != nil {
			return nil, fmt.Errorf("unable to compile filter template: %w", err)
		}
	}
	er.FilterRegex, err = regexp.Compile(cfg.FilterRegex)
	if err != nil {
		return nil, fmt.Errorf("unable to compile regex: %w", err)
	}
	var slackClient SlackClient
	if cfg.SlackChannel == "" {
		log.Warn(ctx, "no slack channel set.  Just sending messages to stdout")
	} else if cfg.SlackClientSecret == "" {
		log.Warn(ctx, "no slack API secret set.  Just sending messages to stdout")
	} else {
		slackClient = m.SlackConstructor(cfg.SlackClientSecret)
		resp, err := slackClient.AuthTestContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to verify slack auth: %w", err)
		}
		log.Info(ctx, "slack setup", zap.String("team", resp.Team), zap.String("user", resp.User))
	}
	return &Server{
		log:         log,
		EventRule:   er,
		slackClient: slackClient,
		slackChan:   cfg.SlackChannel,
	}, nil
}

type Server struct {
	log         *zapctx.Logger
	slackClient SlackClient
	slackChan   string
	EventRule   *EventRule
}

type EventRule struct {
	FilterTemplate *template.Template
	FilterRegex    *regexp.Regexp
	MsgToSend      *template.Template
}

func (s *Server) parse(ctx context.Context, input map[string]interface{}) (string, error) {
	if s.EventRule.FilterTemplate != nil {
		var filterBuf bytes.Buffer
		if err := s.EventRule.FilterTemplate.Execute(&filterBuf, input); err != nil {
			return "", fmt.Errorf("unable to execute filter template: %w", err)
		}
		if !s.EventRule.FilterRegex.MatchString(filterBuf.String()) {
			s.log.Info(ctx, "msg filter does not match")
			return "", nil
		}
	}
	var buf bytes.Buffer
	if err := s.EventRule.MsgToSend.Execute(&buf, input); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type SlackClient interface {
	SendMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (_channel string, _timestamp string, _text string, err error)
	AuthTestContext(ctx context.Context) (response *slack.AuthTestResponse, err error)
}

var _ SlackClient = &slack.Client{}

func (s *Server) handleRequest(ctx context.Context, input map[string]interface{}) error {
	s.log.Info(ctx, "Got an input")
	msg, err := s.parse(ctx, input)
	if err != nil {
		s.log.IfErr(err).Warn(ctx, "unable to execute template")
		return err
	}
	s.log.Info(ctx, "parsed a msg", zap.String("msg", msg))
	if s.slackClient == nil {
		s.log.Info(ctx, msg)
		return nil
	}
	if msg == "" {
		s.log.Debug(ctx, "empty message is skipped")
		return nil
	}
	_, _, _, err = s.slackClient.SendMessageContext(withText(ctx, msg), s.slackChan, slack.MsgOptionText(msg, false))
	s.log.IfErr(err).Warn(ctx, "unable to post message")
	return err
}

type ctxKey int

const (
	text ctxKey = iota
)

func withText(ctx context.Context, s string) context.Context {
	return context.WithValue(ctx, text, s)
}

func getText(ctx context.Context) string {
	if s := ctx.Value(text); s != nil {
		return s.(string)
	}
	return ""
}
