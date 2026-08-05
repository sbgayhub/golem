module golem_plugin_hermes_bridge

go 1.26

require (
	github.com/sbgayhub/golem/sdk v0.1.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/fatih/color v1.19.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.8.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/oklog/run v1.2.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/phsym/console-slog v0.3.1 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260615183401-62b3387ff324 // indirect
	google.golang.org/grpc v1.81.1 // indirect
)

// 使用本仓 sdk：VideoData 含 Thumb 字段（发布版 v0.1.1 无此字段时无法 message.Send 带封面）
replace github.com/sbgayhub/golem/sdk => ../../sdk
