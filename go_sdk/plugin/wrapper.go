package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/sbgayhub/golem/sdk/cdn"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/message"
	"google.golang.org/grpc"
)

// wrapper 插件实现的GRPC包装
type wrapper struct {
	plugin.NetRPCUnsupportedPlugin
	impl Plugin
}

func (w wrapper) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	RegisterPluginServiceServer(s, &server{impl: w.impl, broker: broker})
	return nil
}

func (w wrapper) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return &client{
		client: NewPluginServiceClient(conn),
		broker: broker,
	}, nil
}

// grpc客户端，在host中使用
type client struct {
	client     PluginServiceClient
	broker     *plugin.GRPCBroker
	pluginName string
}

func (c client) GetMetadata() *Metadata {
	if data, err := c.client.GetMetadata(context.Background(), &GetMetadata_Request{}); err != nil {
		return nil
	} else {
		if data.Value != nil {
			c.pluginName = data.Value.Name
		}
		return data.Value
	}
}

func (c client) GetSubscriptions() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if data, err := c.client.GetSubscriptions(ctx, &GetSubscriptions_Request{}); err != nil {
		return nil
	} else {
		return data.Values
	}
}

func (c client) OnEvent(event *Event) (bool, error) {
	if data, err := c.client.OnEvent(context.Background(), &OnEvent_Request{Value: event}); err != nil {
		return false, err
	} else if data.Message != "" {
		return false, errors.New(data.Message)
	} else {
		return data.Result, nil
	}
}

func (c client) GetCapabilities() []string {
	if data, err := c.client.GetCapabilities(context.Background(), &GetCapabilities_Request{}); err != nil {
		return nil
	} else {
		return data.Values
	}
}

func (c client) OnCall(method string, args any) (any, error) {
	if data, err := c.client.OnCall(context.Background(), &OnCall_Request{}); err != nil {
		return nil, err
	} else {
		return data, nil
	}
}
func (c client) GetCommands() []string {
	if data, err := c.client.GetCommands(context.Background(), &GetCommands_Request{}); err != nil {
		return nil
	} else {
		return data.Values
	}
}

func (c client) GetCommandSchemas() []*CommandSchema {
	if data, err := c.client.GetCommands(context.Background(), &GetCommands_Request{}); err != nil {
		return nil
	} else {
		return data.Schemas
	}
}

func (c client) OnCommand(command *Command) (string, error) {
	resp, err := c.client.OnCommand(context.Background(), &OnCommand_Request{Value: command})
	if err != nil {
		return "", err
	}
	if resp != nil && resp.Message != "" {
		return "", errors.New(resp.Message)
	}
	return resp.GetResult(), nil
}

func (c client) OnLoad() error {
	if data, err := c.client.OnLoad(context.Background(), &OnLifecycle_Request{}); err != nil {
		return err
	} else if data != nil && data.Value != "" {
		return errors.New(data.Value)
	}
	return nil
}

func (c client) OnUnload() error {
	if data, err := c.client.OnUnload(context.Background(), &OnLifecycle_Request{}); err != nil {
		return err
	} else if data != nil && data.Value != "" {
		return errors.New(data.Value)
	}
	return nil
}

func (c client) OnEnable() error {
	if data, err := c.client.OnEnabled(context.Background(), &OnLifecycle_Request{}); err != nil {
		return err
	} else if data != nil && data.Value != "" {
		return errors.New(data.Value)
	}
	return nil
}

func (c client) OnDisable() error {
	if data, err := c.client.OnDisabled(context.Background(), &OnLifecycle_Request{}); err != nil {
		return err
	} else if data != nil && data.Value != "" {
		return errors.New(data.Value)
	}
	return nil
}

func (c client) GetAbilities() []string {
	if abilities, err := c.client.GetAbilities(context.Background(), &GetAbilities_Request{}); err != nil {
		return nil
	} else {
		return abilities.Values
	}
}

func (c client) InjectAbilities(abilities []string) error {
	slog.Info("[host wrapper] 插件需要能力", "abilities", abilities)
	request := &InjectAbility_Request{}

	for _, ability := range abilities {
		switch ability {
		case "message":
			id := c.broker.NextId()
			request.Message = id
			go c.broker.AcceptAndServe(id, func(options []grpc.ServerOption) *grpc.Server {
				s := grpc.NewServer(options...)
				message.RegisterMessageServiceServer(s, &message.Server{Impl: message.Instance})
				return s
			})
		case "contact":
			id := c.broker.NextId()
			request.Contact = id
			go c.broker.AcceptAndServe(id, func(options []grpc.ServerOption) *grpc.Server {
				s := grpc.NewServer(options...)
				contact.RegisterContactServiceServer(s, nil)
				return s
			})
		case "session", "caller", "config":
			id := c.broker.NextId()
			go c.broker.AcceptAndServe(id, func(options []grpc.ServerOption) *grpc.Server {
				s := grpc.NewServer(options...)
				RegisterHostServiceServer(s, HostServiceImpl)
				return s
			})
			switch ability {
			case "session":
				request.Session = id
			case "caller":
				request.Caller = id
			case "config":
				request.Config = id
			}
		}
	}

	if _, err := c.client.InjectAbilities(context.Background(), request); err != nil {
		slog.Error("[host wrapper] 注入能力失败", "err", err)
		return err
	}
	return nil
}

// GetDefaultConfig 获取插件默认配置（宿主调用）
func (c client) GetDefaultConfig() ([]byte, error) {
	resp, err := c.client.GetDefaultConfig(context.Background(), &GetDefaultConfig_Request{})
	slog.Debug("[host wrapper] 获取插件配置", "resp", resp, "err", err)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// SetConfig 向插件注入配置（宿主调用）
func (c client) SetConfig(data []byte) error {
	resp, err := c.client.SetConfig(context.Background(), &SetConfig_Request{Data: data})
	if err != nil {
		return err
	}
	if !resp.Result {
		return errors.New("[host wrapper] 注入配置失败")
	}
	return err
}

// grpc服务端，在plugin中使用
type server struct {
	impl      Plugin
	abilities []string
	broker    *plugin.GRPCBroker
	UnimplementedPluginServiceServer
}

func (s *server) GetMetadata(ctx context.Context, request *GetMetadata_Request) (*GetMetadata_Response, error) {
	return &GetMetadata_Response{Value: s.impl.GetMetadata()}, nil
}

func (s *server) OnCall(ctx context.Context, request *OnCall_Request) (*OnCall_Response, error) {
	if cp, ok := s.impl.(CalledPlugin); ok {
		// 将 CallEvent.Args(string) 解析为 map[string]string
		var args map[string]string
		if request.Value != nil && request.Value.Args != "" {
			_ = json.Unmarshal([]byte(request.Value.Args), &args)
		}
		result, err := cp.OnCall(request.Value.Method, args)
		if err != nil {
			return &OnCall_Response{Message: err.Error()}, nil
		}
		return &OnCall_Response{Result: result}, nil
	}
	return nil, errors.New("[plugin wrapper] 插件不支持调用")
}

func (s *server) OnEvent(ctx context.Context, request *OnEvent_Request) (*OnEvent_Response, error) {
	if ep, ok := s.impl.(EventPlugin); ok {
		if res, err := ep.OnEvent(request.Value); err != nil {
			return &OnEvent_Response{Result: res, Message: err.Error()}, nil
		} else {
			return &OnEvent_Response{Result: res}, nil
		}
	}
	return nil, errors.New("[plugin wrapper] 插件不支持事件")
}

func (s *server) OnCommand(ctx context.Context, request *OnCommand_Request) (*OnCommand_Response, error) {
	if cp, ok := s.impl.(CommandPlugin); ok {
		result, err := cp.OnCommand(request.Value)
		if err != nil {
			return &OnCommand_Response{Message: err.Error()}, nil
		}
		return &OnCommand_Response{Result: result}, nil
	}
	return nil, errors.New("[plugin wrapper] 插件不支持命令")
}

func (s *server) GetCapabilities(ctx context.Context, request *GetCapabilities_Request) (*GetCapabilities_Response, error) {
	if cp, ok := s.impl.(CalledPlugin); ok {
		return &GetCapabilities_Response{Values: cp.GetCapabilities()}, nil
	}
	return nil, errors.New("[plugin wrapper] 插件不支持获取调用能力事件")
}

func (s *server) GetSubscriptions(ctx context.Context, request *GetSubscriptions_Request) (*GetSubscriptions_Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if ep, ok := s.impl.(EventPlugin); ok {
		return &GetSubscriptions_Response{Values: ep.GetSubscriptions()}, nil
	}
	return nil, errors.New("[plugin wrapper] 插件不支持获取订阅事件")
}

func (s *server) GetCommands(ctx context.Context, request *GetCommands_Request) (*GetCommands_Response, error) {
	if cp, ok := s.impl.(CommandPlugin); ok {
		schemas := CommandSchemas()
		if sp, ok := s.impl.(CommandSchemaProvider); ok {
			schemas = sp.GetCommandSchemas()
		}
		return &GetCommands_Response{Values: cp.GetCommands(), Schemas: schemas}, nil
	}
	return nil, errors.New("[plugin wrapper] 插件不支持获取调用能力事件")
}

func (s *server) OnLoad(ctx context.Context, request *OnLifecycle_Request) (*OnLifecycle_Response, error) {
	if lc, ok := s.impl.(Lifecycle); ok {
		if err := lc.OnLoad(); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *server) OnUnload(ctx context.Context, request *OnLifecycle_Request) (*OnLifecycle_Response, error) {
	if lc, ok := s.impl.(Lifecycle); ok {
		if err := lc.OnUnload(); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *server) OnEnabled(ctx context.Context, request *OnLifecycle_Request) (*OnLifecycle_Response, error) {
	if lc, ok := s.impl.(Lifecycle); ok {
		if err := lc.OnEnable(); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *server) OnDisabled(ctx context.Context, request *OnLifecycle_Request) (*OnLifecycle_Response, error) {
	if lc, ok := s.impl.(Lifecycle); ok {
		if err := lc.OnDisable(); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *server) GetAbilities(ctx context.Context, request *GetAbilities_Request) (*GetAbilities_Response, error) {
	s.abilities = []string{}
	// 检查插件是否声明了cdn能力
	if check(s.impl, reflect.TypeFor[cdn.Ability]()) {
		s.abilities = append(s.abilities, "cdn")
	}

	if check(s.impl, reflect.TypeFor[message.Ability]()) {
		s.abilities = append(s.abilities, "message")
	}

	if check(s.impl, reflect.TypeFor[SessionAbility]()) {
		s.abilities = append(s.abilities, "session")
	}

	if check(s.impl, reflect.TypeFor[CallerAbility]()) {
		s.abilities = append(s.abilities, "caller")
	}

	if findConfigField(s.impl).IsValid() {
		s.abilities = append(s.abilities, "config")
	}

	return &GetAbilities_Response{Values: s.abilities}, nil
}

func (s *server) InjectAbilities(ctx context.Context, request *InjectAbility_Request) (*InjectAbility_Response, error) {
	// 判断插件是否嵌入能力
	for _, ability := range s.abilities {
		switch ability {
		case "cdn": // 插件声明了cdn能力
			if request.Cdn == 0 { // 宿主传递的cdn能力服务地址为空
				slog.Error("[plugin wrapper] 宿主CDN服务地址为空")
				return &InjectAbility_Response{Value: "[plugin wrapper] 宿主CDN服务地址为空"}, errors.New("[plugin wrapper] 宿主CDN服务地址为空")
			}
			// 连接到宿主CDN能力服务
			if conn, err := s.broker.Dial(request.Message); err != nil {
				slog.Error("[plugin wrapper] 连接宿主CDN grpc服务失败", "err", err)
				return &InjectAbility_Response{Value: err.Error()}, err
			} else {
				client := cdn.NewCDNServiceClient(conn)                                        // 创建一个CDNService的grpc客户端
				inject(s.impl, reflect.TypeFor[cdn.Ability](), cdn.GRPCClient{Client: client}) // 将grpc client注入给插件
			}

		case "message":
			if request.Message == 0 {
				slog.Error("[plugin wrapper] 宿主消息管理服务地址为空")
				return &InjectAbility_Response{Value: "[plugin wrapper] 宿主消息管理服务地址为空"}, errors.New("[plugin wrapper] 宿主消息管理服务地址为空")
			}
			if conn, err := s.broker.Dial(request.Message); err != nil {
				slog.Error("[plugin wrapper] 连接宿主消息管理grpc服务失败", "err", err)
				return &InjectAbility_Response{Value: err.Error()}, err
			} else {
				client := message.NewMessageServiceClient(conn)
				inject(s.impl, reflect.TypeFor[message.Ability](), message.Client{Client: client})
			}

		case "session":
			if request.Session == 0 {
				slog.Error("[plugin wrapper] 宿主会话管理服务地址为空")
				return &InjectAbility_Response{Value: "[plugin wrapper] 宿主会话管理服务地址为空"}, errors.New("[plugin wrapper] 宿主会话管理服务地址为空")
			}
			if conn, err := s.broker.Dial(request.Session); err != nil {
				slog.Error("[plugin wrapper] 连接宿主 HostService 失败", "err", err)
				return &InjectAbility_Response{Value: err.Error()}, err
			} else {
				client := NewHostServiceClient(conn)
				inject(s.impl, reflect.TypeFor[SessionAbility](), SessionClient{Client: client})
			}

		case "caller":
			if request.Caller == 0 {
				slog.Error("[plugin wrapper] 宿主调用服务地址为空")
				return &InjectAbility_Response{Value: "[plugin wrapper] 宿主调用服务地址为空"}, errors.New("[plugin wrapper] 宿主调用服务地址为空")
			}
			if conn, err := s.broker.Dial(request.Caller); err != nil {
				slog.Error("[plugin wrapper] 连接宿主 HostService 失败", "err", err)
				return &InjectAbility_Response{Value: err.Error()}, err
			} else {
				client := NewHostServiceClient(conn)
				inject(s.impl, reflect.TypeFor[CallerAbility](), CallerClient{Client: client})
			}

		case "config":
			if request.Config == 0 {
				slog.Error("[plugin wrapper] 宿主配置管理服务地址为空")
				return &InjectAbility_Response{Value: "[plugin wrapper] 宿主配置管理服务地址为空"}, errors.New("[plugin wrapper] 宿主配置管理服务地址为空")
			}
			if conn, err := s.broker.Dial(request.Config); err != nil {
				slog.Error("[plugin wrapper] 连接宿主配置管理服务失败", "err", err)
				return &InjectAbility_Response{Value: err.Error()}, err
			} else {
				client := NewHostServiceClient(conn)
				injectConfigSave(s.impl, func(pluginName string, data []byte) error {
					resp, err := client.SaveConfig(context.Background(), &SaveConfig_Request{
						PluginId: pluginName,
						Data:     data,
					})
					if err != nil {
						return err
					}
					if resp.Message != "" {
						return errors.New(resp.Message)
					}
					return nil
				})
			}
		}
	}
	return nil, nil
}

func (s *server) GetDefaultConfig(ctx context.Context, _ *GetDefaultConfig_Request) (*GetDefaultConfig_Response, error) {
	slog.Debug("[plugin wrapper] 开始获取插件配置")
	field := findConfigField(s.impl)
	if !field.IsValid() {
		return &GetDefaultConfig_Response{}, nil
	}
	slog.Debug("[plugin wrapper] 插件默认配置", "config", field.Interface())
	data, err := json.Marshal(field.Interface())
	if err != nil {
		return nil, err
	}
	return &GetDefaultConfig_Response{Data: data}, nil
}

func (s *server) SetConfig(ctx context.Context, req *SetConfig_Request) (*SetConfig_Response, error) {
	field := findConfigField(s.impl)
	if !field.IsValid() {
		return &SetConfig_Response{Result: false}, nil
	}
	slog.Debug("[plugin wrapper] 注入插件配置", "config", string(req.Data))
	if err := json.Unmarshal(req.Data, field.Addr().Interface()); err != nil {
		return &SetConfig_Response{Result: false}, err
	}
	return &SetConfig_Response{Result: true}, nil
}
