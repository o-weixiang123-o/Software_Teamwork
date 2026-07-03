package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/config"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/localtools"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/mcpclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/modelclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/toolclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

// Runtime owns the long-lived model and tool dependencies shared by the CLI
// and HTTP server.
type Runtime struct {
	Runner       *agent.Runner
	SystemPrompt string
	remoteTools  *mcpclient.Client
}

func New(ctx context.Context, cfg config.Config, observer agent.Observer) (*Runtime, error) {
	local, err := localtools.New(localtools.Config{
		WorkDir:           cfg.WorkDir,
		MaxFileBytes:      cfg.MaxFileBytes,
		MaxOutputBytes:    cfg.MaxToolResultBytes,
		EnableCommandTool: cfg.EnableCommandTool,
		CommandTimeout:    cfg.CommandTimeout,
	})
	if err != nil {
		return nil, err
	}

	providers := []agent.ToolClient{local}
	var remoteTools *mcpclient.Client
	if cfg.MCPTransport != config.TransportDisabled {
		remoteTools, err = mcpclient.Connect(ctx, mcpclient.Config{
			Transport:   cfg.MCPTransport,
			Command:     cfg.MCPServerCommand,
			Args:        cfg.MCPServerArgs,
			Endpoint:    cfg.MCPServerURL,
			Token:       cfg.MCPServerToken,
			TokenHeader: cfg.MCPServerTokenHeader,
		})
		if err != nil {
			return nil, err
		}
		providers = append(providers, remoteTools)
	}

	tools, err := toolclient.New(providers...)
	if err != nil {
		closeRemote(remoteTools)
		return nil, err
	}
	model, err := modelclient.New(modelclient.Config{
		Endpoint:    cfg.AIGatewayURL,
		Token:       cfg.AIGatewayToken,
		TokenHeader: cfg.AIGatewayTokenHeader,
		Model:       cfg.ModelID,
		ProfileID:   cfg.AIGatewayProfileID,
		MaxTokens:   cfg.MaxTokens,
		Timeout:     cfg.ModelTimeout,
		Stream:      cfg.AIGatewayStream,
	})
	if err != nil {
		closeRemote(remoteTools)
		return nil, err
	}
	runner, err := agent.NewRunner(model, tools, agent.Config{
		MaxIterations:          cfg.MaxIterations,
		ToolTimeout:            cfg.MCPToolTimeout,
		MaxToolResultBytes:     cfg.MaxToolResultBytes,
		Observer:               observer,
		ReasoningFilterFactory: service.NewReasoningFilter,
		ToolResultPolicy:       service.KnowledgeRetrievalStopPolicy,
	})
	if err != nil {
		closeRemote(remoteTools)
		return nil, err
	}
	return &Runtime{Runner: runner, SystemPrompt: cfg.SystemPrompt, remoteTools: remoteTools}, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.remoteTools == nil {
		return nil
	}
	return r.remoteTools.Close()
}

func closeRemote(client *mcpclient.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("close MCP client after startup failure", "service", "qa", "dependency", "mcp", "error", err)
	}
}
