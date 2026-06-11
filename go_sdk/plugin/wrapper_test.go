package plugin

import "testing"

type testCommandPlugin struct{}

func (testCommandPlugin) GetMetadata() *Metadata { return &Metadata{Name: "test"} }

func (testCommandPlugin) GetCommands() []string { return CommandCommands() }

func (testCommandPlugin) OnCommand(cmd *Command) (string, error) { return DispatchCommand(cmd) }

type testCommandInput struct {
	_    struct{} `cmd:"test run" help:"测试命令"`
	Path string   `arg:"path" help:"路径" required:"true"`
}

type testCommandContextInput struct {
	_       struct{} `cmd:"test context" help:"测试命令上下文"`
	Command *Command
}

type testPointerCommandInput struct {
	_       struct{} `cmd:"test set" help:"测试指针参数"`
	Enabled *bool    `flag:"e,enabled" help:"是否启用"`
	Limit   *int32   `flag:"l,limit" help:"限制值"`
	Note    *string  `flag:"n,note" help:"备注"`
}

func TestRegisterCommandBindsHandlerInputAutomatically(t *testing.T) {
	defaultCommandRegistry = NewCommandRegistry()
	t.Cleanup(func() {
		defaultCommandRegistry = NewCommandRegistry()
	})

	var got testCommandInput
	if err := RegisterCommand(func(input testCommandInput) (string, error) {
		got = input
		return input.Path, nil
	}); err != nil {
		t.Fatalf("register command: %v", err)
	}

	result, err := DispatchCommand(&Command{
		Main:        "test",
		Sub:         "run",
		Positionals: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("dispatch command: %v", err)
	}
	if result != "demo" {
		t.Fatalf("unexpected result: %q", result)
	}
	if got.Path != "demo" {
		t.Fatalf("handler input not bound, got %+v", got)
	}
}

func TestBindInjectsRawCommand(t *testing.T) {
	cmd := &Command{Main: "test", Sub: "context", Raw: "/test context"}
	got, err := Bind[testCommandContextInput](cmd)
	if err != nil {
		t.Fatalf("bind command context: %v", err)
	}
	if got.Command != cmd {
		t.Fatalf("raw command not injected")
	}
}

func TestRegisterCommandToUsesProvidedRegistry(t *testing.T) {
	registry := NewCommandRegistry()
	if err := RegisterCommandTo(registry, func(input testCommandInput) (string, error) {
		return input.Path, nil
	}); err != nil {
		t.Fatalf("register command to registry: %v", err)
	}

	result, err := registry.Dispatch(&Command{
		Main:        "test",
		Sub:         "run",
		Positionals: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("dispatch command: %v", err)
	}
	if result != "demo" {
		t.Fatalf("unexpected result: %q", result)
	}
	if len(CommandCommands()) != 0 {
		t.Fatalf("default registry should remain empty: %+v", CommandCommands())
	}
}

func TestServerGetCommandsFallsBackToDefaultRegistrySchemas(t *testing.T) {
	defaultCommandRegistry = NewCommandRegistry()
	t.Cleanup(func() {
		defaultCommandRegistry = NewCommandRegistry()
	})

	if err := RegisterCommand(func(input testCommandInput) (string, error) {
		return input.Path, nil
	}); err != nil {
		t.Fatalf("register command: %v", err)
	}

	srv := &server{impl: testCommandPlugin{}}
	resp, err := srv.GetCommands(nil, &GetCommands_Request{})
	if err != nil {
		t.Fatalf("get commands: %v", err)
	}
	if len(resp.GetValues()) != 1 {
		t.Fatalf("unexpected command count: %d", len(resp.GetValues()))
	}
	if got := resp.GetValues()[0]; got != "test" {
		t.Fatalf("unexpected command name: %q", got)
	}
	if len(resp.GetSchemas()) != 1 {
		t.Fatalf("unexpected schema count: %d", len(resp.GetSchemas()))
	}
	schema := resp.GetSchemas()[0]
	if schema.GetMain() != "test" || schema.GetSub() != "run" {
		t.Fatalf("unexpected schema command: %s %s", schema.GetMain(), schema.GetSub())
	}
}

func TestBindSupportsPointerFields(t *testing.T) {
	got, err := Bind[testPointerCommandInput](&Command{
		Main: "test",
		Sub:  "set",
		Args: map[string]string{
			"enabled": "false",
			"limit":   "30",
		},
	})
	if err != nil {
		t.Fatalf("bind command: %v", err)
	}
	if got.Enabled == nil || *got.Enabled {
		t.Fatalf("enabled not bound as false pointer: %#v", got.Enabled)
	}
	if got.Limit == nil || *got.Limit != 30 {
		t.Fatalf("limit not bound as int32 pointer: %#v", got.Limit)
	}
	if got.Note != nil {
		t.Fatalf("unset note should remain nil: %#v", got.Note)
	}

	empty, err := Bind[testPointerCommandInput](&Command{Main: "test", Sub: "set"})
	if err != nil {
		t.Fatalf("bind empty command: %v", err)
	}
	if empty.Enabled != nil || empty.Limit != nil || empty.Note != nil {
		t.Fatalf("unset pointer fields should remain nil: %+v", empty)
	}

	emptyNote, err := Bind[testPointerCommandInput](&Command{
		Main: "test",
		Sub:  "set",
		Args: map[string]string{"note": ""},
	})
	if err != nil {
		t.Fatalf("bind empty note command: %v", err)
	}
	if emptyNote.Note == nil || *emptyNote.Note != "" {
		t.Fatalf("empty note should bind as empty string pointer: %#v", emptyNote.Note)
	}
}
