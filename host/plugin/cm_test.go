package plugin

import (
	"testing"

	"github.com/sbgayhub/golem/sdk/contact"
	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

func TestCMCommandSchemas(t *testing.T) {
	cm, err := newCMCommandPlugin()
	if err != nil {
		t.Fatalf("new cm command plugin: %v", err)
	}

	if got := cm.GetCommands(); len(got) != 1 || got[0] != "cm" {
		t.Fatalf("unexpected commands: %+v", got)
	}

	var hasContact, hasChatroom bool
	for _, schema := range cm.GetCommandSchemas() {
		if schema.GetMain() != "cm" {
			t.Fatalf("unexpected main command: %s", schema.GetMain())
		}
		switch schema.GetSub() {
		case "contact":
			hasContact = true
			assertOptionalKeyArgument(t, schema)
		case "chatroom":
			hasChatroom = true
			assertOptionalKeyArgument(t, schema)
		}
	}
	if !hasContact || !hasChatroom {
		t.Fatalf("missing schemas: contact=%t chatroom=%t", hasContact, hasChatroom)
	}
}

func TestCMChatroomRejectsNonChatroomSource(t *testing.T) {
	cm := &cmCommandPlugin{}
	_, err := cm.chatroom(cmChatroomCommand{
		Command: &sdk.Command{Sender: &contact.Contact{
			Username: "wxid_xxx",
			Type:     contact.ContactType_CONTACT_TYPE_FRIEND,
		}},
	})
	if err == nil || err.Error() != "该命令仅支持在群聊中使用" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCMChatroomRejectsEmptyChatroomSource(t *testing.T) {
	cm := &cmCommandPlugin{}
	_, err := cm.chatroom(cmChatroomCommand{
		Command: &sdk.Command{Sender: &contact.Contact{
			Type: contact.ContactType_CONTACT_TYPE_CHATROOM,
		}},
	})
	if err == nil || err.Error() != "命令来源群聊为空" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterBuiltinCMAddsCommandIndex(t *testing.T) {
	withPluginState(t)

	if err := registerBuiltinCM(); err != nil {
		t.Fatalf("register builtin cm: %v", err)
	}
	if commandIndex["cm"] == nil {
		t.Fatalf("cm command should be indexed")
	}
	if got := commandIndex["cm"].Name; got != "cm" {
		t.Fatalf("unexpected command owner: %s", got)
	}
}

func assertOptionalKeyArgument(t *testing.T, schema *sdk.CommandSchema) {
	t.Helper()

	args := schema.GetArguments()
	if len(args) != 1 {
		t.Fatalf("%s should have one argument, got %+v", schema.GetSub(), args)
	}
	if args[0].GetName() != "key" {
		t.Fatalf("%s argument should be key, got %s", schema.GetSub(), args[0].GetName())
	}
	if args[0].GetRequired() {
		t.Fatalf("%s key argument should be optional", schema.GetSub())
	}
}
