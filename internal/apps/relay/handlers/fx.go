package handlers

import (
	"github.com/normahq/relay/internal/apps/relay/agent"
	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	"github.com/normahq/relay/internal/apps/relay/session"
	"github.com/normahq/relay/internal/apps/relay/tgbotkit"
	"github.com/rs/zerolog"
	"github.com/tgbotkit/client"
	"go.uber.org/fx"
)

// Module provides handlers for the relay bot.
var Module = fx.Module("relay_handlers",
	fx.Provide(
		agent.NewBuilder,
		agent.NewRuntimeManager,
		session.NewManager,
		fx.Annotate(
			func(
				tgClient client.ClientWithResponsesInterface,
				logger zerolog.Logger,
				formattingMode string,
			) *messenger.Messenger {
				m := messenger.NewMessenger(tgClient, logger)
				m.SetAgentReplyFormattingMode(formattingMode)
				return m
			},
			fx.ParamTags(``, ``, `name:"relay_telegram_formatting_mode"`),
		),
		relaytelegram.NewAdapter,
		NewTurnDispatcher,
		NewStartHandler,
		NewRelayHandler,
		NewCommandHandler,
		NewUserHandler,
		fx.Annotate(
			registerStartHandler,
			fx.As(new(tgbotkit.Handler)),
			fx.ResultTags(`group:"bot_handlers"`),
		),
		fx.Annotate(
			registerRelayHandler,
			fx.As(new(tgbotkit.Handler)),
			fx.ResultTags(`group:"bot_handlers"`),
		),
		fx.Annotate(
			registerCommandHandler,
			fx.As(new(tgbotkit.Handler)),
			fx.ResultTags(`group:"bot_handlers"`),
		),
		fx.Annotate(
			registerUserHandler,
			fx.As(new(tgbotkit.Handler)),
			fx.ResultTags(`group:"bot_handlers"`),
		),
	),
	fx.Invoke(WireHandlers),
)

// WireHandlers connects the start handler to the relay handler.
func WireHandlers(start *StartHandler, relay *RelayHandler) {
	start.SetRelayHandler(relay)
}

func registerStartHandler(h *StartHandler) tgbotkit.Handler {
	return h
}

func registerRelayHandler(h *RelayHandler) tgbotkit.Handler {
	return h
}

func registerCommandHandler(h *CommandHandler) tgbotkit.Handler {
	return h
}

func registerUserHandler(h *userHandler) tgbotkit.Handler {
	return h
}
