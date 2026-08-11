package biz

import (
	"context"
	"testing"
)

func TestChannelCreate_WecomBotRequiresBotIDAndSecretWhenEnabled(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	_, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-1",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "bot-1",
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Create error = %v, want INVALID_ARGUMENT", err)
	}

	meta, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-2",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "bot-1",
		BotSecret: "sec-1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if meta.BotID != "bot-1" || meta.BotSecret != "sec-1" {
		t.Fatalf("got bot=%q secret=%q", meta.BotID, meta.BotSecret)
	}
}

func TestChannelCreate_WebhookRejectsInvalidReplyMode(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	_, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID:        "wh-1",
		Type:             "webhook",
		Enabled:          true,
		DefaultReplyMode: "fast",
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Create error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestChannelUpdate_EmptySecretKeepsExistingForWecomBot(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-keep",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "bot-1",
		BotSecret: "original-secret",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := uc.Update(context.Background(), created.ID, map[string]any{
		"enabled": true,
		"secret":  "",
		"bot_id":  "bot-1",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.BotSecret != "original-secret" {
		t.Fatalf("BotSecret = %q, want original-secret", updated.BotSecret)
	}
}

func TestChannelUpdate_MapsSecretToBotSecret(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-map",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "bot-1",
		BotSecret: "old",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := uc.Update(context.Background(), created.ID, map[string]any{
		"secret": "new-secret",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.BotSecret != "new-secret" {
		t.Fatalf("BotSecret = %q, want new-secret", updated.BotSecret)
	}
}

func TestChannelUpdate_EnableWecomBotWithoutSecretFails(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-off",
		Type:      "wecom_bot",
		Enabled:   false,
		BotID:     "",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = uc.Update(context.Background(), created.ID, map[string]any{
		"enabled": true,
		"bot_id":  "bot-1",
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Update error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestChannelUpdate_CoercesBotNamesFromAnySlice(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)

	created, err := uc.Create(context.Background(), &ChannelCreate{
		ChannelID: "wb-names",
		Type:      "wecom_bot",
		Enabled:   true,
		BotID:     "bot-1",
		BotSecret: "sec",
		BotNames:  []string{"old"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := uc.Update(context.Background(), created.ID, map[string]any{
		"bot_names": []any{"Alice", "Bob"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.BotNames) != 2 || updated.BotNames[0] != "Alice" || updated.BotNames[1] != "Bob" {
		t.Fatalf("BotNames = %v, want [Alice Bob]", updated.BotNames)
	}

	_, err = uc.Update(context.Background(), created.ID, map[string]any{
		"bot_names": "not-an-array",
	})
	if !isReason(err, "INVALID_ARGUMENT") {
		t.Fatalf("Update error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestListGatewayChannels_IncludesDisabled(t *testing.T) {
	repo := &fakeChannelRepo{byID: map[string]*ChannelMeta{}}
	uc := newChannelUsecaseForTest(repo)
	ctx := context.Background()

	if _, err := uc.Create(ctx, &ChannelCreate{
		ChannelID: "wh-on",
		Type:      "webhook",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("Create webhook: %v", err)
	}
	if _, err := uc.Create(ctx, &ChannelCreate{
		ChannelID: "wb-off",
		Type:      "wecom_bot",
		Enabled:   false,
		BotID:     "b",
		BotSecret: "s",
	}); err != nil {
		t.Fatalf("Create wecom_bot: %v", err)
	}
	if _, err := uc.Create(ctx, &ChannelCreate{
		ChannelID: "web-1",
		Type:      "web",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("Create web: %v", err)
	}

	list, err := uc.ListGatewayChannels(ctx)
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	seen := map[string]bool{}
	for _, ch := range list {
		seen[ch.ChannelID] = true
		if ch.Type != "webhook" && ch.Type != "wecom_bot" {
			t.Fatalf("unexpected type %q", ch.Type)
		}
	}
	if !seen["wh-on"] || !seen["wb-off"] {
		t.Fatalf("seen = %v, want wh-on and wb-off", seen)
	}
}
