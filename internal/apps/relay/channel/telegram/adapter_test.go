package telegram

import (
	"testing"

	"github.com/tgbotkit/client"
	"github.com/tgbotkit/runtime/events"
	"github.com/tgbotkit/runtime/messagetype"
)

const testMessageText = "hello"

func TestMessageContextFromEvent_PrivateChatIgnoresMessageThreadID(t *testing.T) {
	topicID := 523431
	isTopicMessage := false

	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId:       11,
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
			Text: textPtr(testMessageText),
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	assertPrivateMessageContext(t, got, 0, "tg-2317500-0")
}

func TestMessageContextFromEvent_SupergroupPreservesMessageThreadID(t *testing.T) {
	topicID := 77

	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId:       21,
			MessageThreadId: &topicID,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if got.TopicID != 77 {
		t.Fatalf("topic_id = %d, want 77 for supergroup topic", got.TopicID)
	}
	if got.ProgressPolicy.Thinking {
		t.Fatalf("progress_policy.thinking = %v, want false for supergroup chat", got.ProgressPolicy.Thinking)
	}
	if !got.ProgressPolicy.Typing {
		t.Fatalf("progress_policy.typing = %v, want true for supergroup chat", got.ProgressPolicy.Typing)
	}
	if got.Locator.SessionID != "tg--1009001-77" {
		t.Fatalf("session_id = %q, want tg--1009001-77", got.Locator.SessionID)
	}
}

func TestMessageContextFromEvent_SupergroupPreservesMessageThreadIDWhenNonTopicFlagFalse(t *testing.T) {
	topicID := 88
	isTopicMessage := false

	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId:       22,
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if got.TopicID != 88 {
		t.Fatalf("topic_id = %d, want 88 for supergroup thread", got.TopicID)
	}
	if got.Locator.SessionID != "tg--1009001-88" {
		t.Fatalf("session_id = %q, want tg--1009001-88", got.Locator.SessionID)
	}
}

func TestMessageContextFromEvent_PrivateTopicPreservesMessageThreadID(t *testing.T) {
	topicID := 523431
	isTopicMessage := true

	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId:       31,
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
			Text: textPtr(testMessageText),
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	assertPrivateMessageContext(t, got, 523431, "tg-2317500-523431")
}

func assertPrivateMessageContext(t *testing.T, got MessageContext, wantTopicID int, wantSessionID string) {
	t.Helper()

	if got.TopicID != wantTopicID {
		t.Fatalf("topic_id = %d, want %d", got.TopicID, wantTopicID)
	}
	if !got.ProgressPolicy.Thinking {
		t.Fatalf("progress_policy.thinking = %v, want true", got.ProgressPolicy.Thinking)
	}
	if !got.ProgressPolicy.Typing {
		t.Fatalf("progress_policy.typing = %v, want true", got.ProgressPolicy.Typing)
	}
	if got.Locator.SessionID != wantSessionID {
		t.Fatalf("session_id = %q, want %q", got.Locator.SessionID, wantSessionID)
	}
}

func TestMessageContextFromEvent_PopulatesReplyMetadataWhenPresent(t *testing.T) {
	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId: 41,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
			ReplyToMessage: &client.Message{
				MessageId: 7,
				From: &client.User{
					Id:    404,
					IsBot: true,
				},
			},
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if !got.IsReply {
		t.Fatalf("is_reply = %v, want true", got.IsReply)
	}
	if got.ReplyToUserID != 404 {
		t.Fatalf("reply_to_user_id = %d, want 404", got.ReplyToUserID)
	}
	if !got.ReplyToIsBot {
		t.Fatalf("reply_to_is_bot = %v, want true", got.ReplyToIsBot)
	}
	if got.ReplyContent != "" {
		t.Fatalf("reply_content = %q, want empty", got.ReplyContent)
	}
}

func TestMessageContextFromEvent_ReplyMetadataEmptyWithoutReply(t *testing.T) {
	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId: 42,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if got.IsReply {
		t.Fatalf("is_reply = %v, want false", got.IsReply)
	}
	if got.ReplyToUserID != 0 {
		t.Fatalf("reply_to_user_id = %d, want 0", got.ReplyToUserID)
	}
	if got.ReplyToIsBot {
		t.Fatalf("reply_to_is_bot = %v, want false", got.ReplyToIsBot)
	}
	if got.ReplyContent != "" {
		t.Fatalf("reply_content = %q, want empty", got.ReplyContent)
	}
}

func TestMessageContextFromEvent_PopulatesReplyContentFromReplyText(t *testing.T) {
	replyText := "quoted message"
	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId: 43,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
			ReplyToMessage: &client.Message{
				MessageId: 8,
				Text:      &replyText,
			},
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if got.ReplyContent != replyText {
		t.Fatalf("reply_content = %q, want %q", got.ReplyContent, replyText)
	}
}

func TestMessageContextFromEvent_UsesReplyCaptionWhenReplyTextMissing(t *testing.T) {
	replyCaption := "photo caption"
	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId: 44,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
			Text: textPtr(testMessageText),
			ReplyToMessage: &client.Message{
				MessageId: 9,
				Caption:   &replyCaption,
			},
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if got.ReplyContent != replyCaption {
		t.Fatalf("reply_content = %q, want %q", got.ReplyContent, replyCaption)
	}
}

func TestMessageContextFromEvent_CopiesEntities(t *testing.T) {
	entities := []client.MessageEntity{
		{Type: "mention", Offset: 6, Length: 8},
	}
	got, ok := (&Adapter{}).MessageContextFromEvent(&events.MessageEvent{
		Message: &client.Message{
			MessageId: 45,
			Chat: client.Chat{
				Id:   -1009001,
				Type: "supergroup",
			},
			From:     &client.User{Id: 101},
			Text:     textPtr("hello @testbot"),
			Entities: &entities,
		},
	})
	if !ok {
		t.Fatal("MessageContextFromEvent() ok = false, want true")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("entities len = %d, want 1", len(got.Entities))
	}
	if got.Entities[0].Type != "mention" || got.Entities[0].Offset != 6 || got.Entities[0].Length != 8 {
		t.Fatalf("entity = %+v, want mention offset=6 length=8", got.Entities[0])
	}
}

func TestCommandContextFromEvent_PrivateChatIgnoresMessageThreadID(t *testing.T) {
	topicID := 523431
	isTopicMessage := false

	got, ok := (&Adapter{}).CommandContextFromEvent(&events.CommandEvent{
		Command: "topic",
		Args:    "codex",
		Message: &client.Message{
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
		},
	})
	if !ok {
		t.Fatal("CommandContextFromEvent() ok = false, want true")
	}
	if got.TopicID != 0 {
		t.Fatalf("topic_id = %d, want 0 for private chat", got.TopicID)
	}
	if got.Locator.SessionID != "tg-2317500-0" {
		t.Fatalf("session_id = %q, want tg-2317500-0", got.Locator.SessionID)
	}
}

func TestCommandContextFromEvent_PrivateTopicPreservesMessageThreadID(t *testing.T) {
	topicID := 523431
	isTopicMessage := true

	got, ok := (&Adapter{}).CommandContextFromEvent(&events.CommandEvent{
		Command: "topic",
		Args:    "codex",
		Message: &client.Message{
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
		},
	})
	if !ok {
		t.Fatal("CommandContextFromEvent() ok = false, want true")
	}
	if got.TopicID != 523431 {
		t.Fatalf("topic_id = %d, want 523431 for private topic command", got.TopicID)
	}
	if got.Locator.SessionID != "tg-2317500-523431" {
		t.Fatalf("session_id = %q, want tg-2317500-523431", got.Locator.SessionID)
	}
}

func TestTopicLifecycleFromEvent_IgnoresPrivateNonTopicChat(t *testing.T) {
	topicID := 523452
	isTopicMessage := false

	_, ok := (&Adapter{}).TopicLifecycleFromEvent(&events.MessageEvent{
		Type: messagetype.ForumTopicCreated,
		Message: &client.Message{
			MessageId:       218,
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
		},
	})
	if ok {
		t.Fatal("TopicLifecycleFromEvent() ok = true, want false for private non-topic chat")
	}
}

func TestTopicLifecycleFromEvent_AcceptsSupergroup(t *testing.T) {
	topicID := 77

	got, ok := (&Adapter{}).TopicLifecycleFromEvent(&events.MessageEvent{
		Type: messagetype.ForumTopicCreated,
		Message: &client.Message{
			MessageId:       42,
			MessageThreadId: &topicID,
			Chat: client.Chat{
				Id:   9001,
				Type: "supergroup",
			},
			From: &client.User{Id: 101},
		},
	})
	if !ok {
		t.Fatal("TopicLifecycleFromEvent() ok = false, want true for supergroup")
	}
	if got.TopicID != 77 {
		t.Fatalf("topic_id = %d, want 77", got.TopicID)
	}
	if got.Locator.SessionID != "tg-9001-77" {
		t.Fatalf("session_id = %q, want tg-9001-77", got.Locator.SessionID)
	}
}

func TestTopicLifecycleFromEvent_AcceptsPrivateTopic(t *testing.T) {
	topicID := 523452
	isTopicMessage := true

	got, ok := (&Adapter{}).TopicLifecycleFromEvent(&events.MessageEvent{
		Type: messagetype.ForumTopicCreated,
		Message: &client.Message{
			MessageId:       218,
			MessageThreadId: &topicID,
			IsTopicMessage:  &isTopicMessage,
			Chat: client.Chat{
				Id:   2317500,
				Type: "private",
			},
			From: &client.User{Id: 2317500},
		},
	})
	if !ok {
		t.Fatal("TopicLifecycleFromEvent() ok = false, want true for private topic")
	}
	if got.TopicID != 523452 {
		t.Fatalf("topic_id = %d, want 523452", got.TopicID)
	}
	if got.Locator.SessionID != "tg-2317500-523452" {
		t.Fatalf("session_id = %q, want tg-2317500-523452", got.Locator.SessionID)
	}
}

func textPtr(s string) *string {
	return &s
}
