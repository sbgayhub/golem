package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/pelletier/go-toml/v2"
	"github.com/sbgayhub/golem/sdk/cdn"
	"github.com/sbgayhub/golem/sdk/chatroom"
	"github.com/sbgayhub/golem/sdk/contact"
	"github.com/sbgayhub/golem/sdk/favor"
	"github.com/sbgayhub/golem/sdk/label"
	"github.com/sbgayhub/golem/sdk/login"
	"github.com/sbgayhub/golem/sdk/message"
	"github.com/sbgayhub/golem/sdk/miniapp"
	"github.com/sbgayhub/golem/sdk/moments"
	"github.com/sbgayhub/golem/sdk/official"
	"github.com/sbgayhub/golem/sdk/payment"
	"github.com/sbgayhub/golem/sdk/report"
	"github.com/sbgayhub/golem/sdk/user"
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

const eventRPCTimeout = time.Minute

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
	ctx, cancel := context.WithTimeout(context.Background(), eventRPCTimeout)
	defer cancel()

	if data, err := c.client.OnEvent(ctx, &OnEvent_Request{Value: event}); err != nil {
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

func (c client) OnCall(capability string, args map[string]string) (string, []byte, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.client.OnCall(context.Background(), &OnCall_Request{
		Capability: capability,
		Args:       data,
	})
	if err != nil {
		return "", nil, err
	}
	if resp != nil && resp.Message != "" {
		return "", nil, errors.New(resp.Message)
	}
	return resp.GetMime(), resp.GetData(), nil
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

func (c client) serveAbility(register func(*grpc.Server)) uint32 {
	id := c.broker.NextId()
	go c.broker.AcceptAndServe(id, func(options []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(options...)
		register(s)
		return s
	})
	return id
}

func (c client) InjectAbilities(abilities []string) error {
	slog.Debug("[host wrapper] 插件需要能力", "abilities", abilities)
	request := &InjectAbility_Request{}

	for _, ability := range abilities {
		switch ability {
		case "cdn":
			request.Cdn = c.serveAbility(func(s *grpc.Server) {
				cdn.RegisterCDNServiceServer(s, &cdn.Server{Impl: cdn.Instance})
			})
		case "message":
			request.Message = c.serveAbility(func(s *grpc.Server) {
				message.RegisterMessageServiceServer(s, &message.Server{Impl: message.Instance})
			})
		case "contact":
			request.Contact = c.serveAbility(func(s *grpc.Server) {
				contact.RegisterContactServiceServer(s, &contact.Server{Impl: contact.Instance})
			})
		case "chatroom":
			request.Chatroom = c.serveAbility(func(s *grpc.Server) {
				chatroom.RegisterChatroomServiceServer(s, &chatroom.Server{Impl: chatroom.Instance})
			})
		case "favor":
			request.Favor = c.serveAbility(func(s *grpc.Server) {
				favor.RegisterFavorServiceServer(s, &favor.Server{Impl: favor.Instance})
			})
		case "label":
			request.Label = c.serveAbility(func(s *grpc.Server) {
				label.RegisterLabelServiceServer(s, &label.Server{Impl: label.Instance})
			})
		case "login":
			request.Login = c.serveAbility(func(s *grpc.Server) {
				login.RegisterLoginServiceServer(s, &login.Server{Impl: login.Instance})
			})
		case "miniapp":
			request.Miniapp = c.serveAbility(func(s *grpc.Server) {
				miniapp.RegisterMiniAppServiceServer(s, &miniapp.Server{Impl: miniapp.Instance})
			})
		case "moments":
			request.Moments = c.serveAbility(func(s *grpc.Server) {
				moments.RegisterMomentsServiceServer(s, &moments.Server{Impl: moments.Instance})
			})
		case "official":
			request.Official = c.serveAbility(func(s *grpc.Server) {
				official.RegisterOfficialServiceServer(s, &official.Server{Impl: official.Instance})
			})
		case "payment":
			request.Payment = c.serveAbility(func(s *grpc.Server) {
				payment.RegisterPaymentServiceServer(s, &payment.Server{Impl: payment.Instance})
			})
		case "report":
			request.Report = c.serveAbility(func(s *grpc.Server) {
				report.RegisterReportServiceServer(s, &report.Server{Impl: report.Instance})
			})
		case "user":
			request.User = c.serveAbility(func(s *grpc.Server) {
				user.RegisterUserServiceServer(s, &user.Server{Impl: user.Instance})
			})
		case "session", "caller", "config":
			id := c.serveAbility(func(s *grpc.Server) {
				RegisterHostServiceServer(s, HostServiceImpl)
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
		var args map[string]string
		if len(request.Args) > 0 {
			_ = json.Unmarshal(request.Args, &args)
		}
		mime, data, err := cp.OnCall(request.Capability, args)
		if err != nil {
			return &OnCall_Response{Message: err.Error()}, nil
		}
		return &OnCall_Response{Mime: mime, Data: data}, nil
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
	return nil, errors.New("[plugin wrapper] 插件不支持获取调用能力")
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
	return nil, errors.New("[plugin wrapper] 插件不支持获取命令")
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

type abilityBinding struct {
	typ   reflect.Type
	name  string
	id    func(*InjectAbility_Request) uint32
	apply func(Plugin, *grpc.ClientConn)
}

var abilityBindings = map[string]abilityBinding{
	"cdn": {
		typ:  reflect.TypeFor[cdn.Ability](),
		name: "CDN服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Cdn },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := cdn.NewCDNServiceClient(conn)
			inject(impl, reflect.TypeFor[cdn.Ability](), cdn.GRPCClient{Client: client})
		},
	},
	"contact": {
		typ:  reflect.TypeFor[contact.Ability](),
		name: "联系人管理服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Contact },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := contact.NewContactServiceClient(conn)
			inject(impl, reflect.TypeFor[contact.Ability](), contact.Client{Client: client})
		},
	},
	"message": {
		typ:  reflect.TypeFor[message.Ability](),
		name: "消息管理服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Message },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := message.NewMessageServiceClient(conn)
			inject(impl, reflect.TypeFor[message.Ability](), message.Client{Client: client})
		},
	},
	"chatroom": {
		typ:  reflect.TypeFor[chatroom.Ability](),
		name: "群组管理服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Chatroom },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := chatroom.NewChatroomServiceClient(conn)
			inject(impl, reflect.TypeFor[chatroom.Ability](), chatroom.Client{Client: client})
		},
	},
	"favor": {
		typ:  reflect.TypeFor[favor.Ability](),
		name: "收藏服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Favor },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := favor.NewFavorServiceClient(conn)
			inject(impl, reflect.TypeFor[favor.Ability](), favor.Client{Client: client})
		},
	},
	"label": {
		typ:  reflect.TypeFor[label.Ability](),
		name: "标签服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Label },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := label.NewLabelServiceClient(conn)
			inject(impl, reflect.TypeFor[label.Ability](), label.Client{Client: client})
		},
	},
	"login": {
		typ:  reflect.TypeFor[login.Ability](),
		name: "登录服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Login },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := login.NewLoginServiceClient(conn)
			inject(impl, reflect.TypeFor[login.Ability](), login.Client{Client: client})
		},
	},
	"miniapp": {
		typ:  reflect.TypeFor[miniapp.Ability](),
		name: "小程序服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Miniapp },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := miniapp.NewMiniAppServiceClient(conn)
			inject(impl, reflect.TypeFor[miniapp.Ability](), miniapp.Client{Client: client})
		},
	},
	"moments": {
		typ:  reflect.TypeFor[moments.Ability](),
		name: "朋友圈服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Moments },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := moments.NewMomentsServiceClient(conn)
			inject(impl, reflect.TypeFor[moments.Ability](), moments.Client{Client: client})
		},
	},
	"official": {
		typ:  reflect.TypeFor[official.Ability](),
		name: "公众号服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Official },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := official.NewOfficialServiceClient(conn)
			inject(impl, reflect.TypeFor[official.Ability](), official.Client{Client: client})
		},
	},
	"payment": {
		typ:  reflect.TypeFor[payment.Ability](),
		name: "支付服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Payment },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := payment.NewPaymentServiceClient(conn)
			inject(impl, reflect.TypeFor[payment.Ability](), payment.Client{Client: client})
		},
	},
	"report": {
		typ:  reflect.TypeFor[report.Ability](),
		name: "状态通知服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Report },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := report.NewReportServiceClient(conn)
			inject(impl, reflect.TypeFor[report.Ability](), report.Client{Client: client})
		},
	},
	"user": {
		typ:  reflect.TypeFor[user.Ability](),
		name: "用户服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.User },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := user.NewUserServiceClient(conn)
			inject(impl, reflect.TypeFor[user.Ability](), user.Client{Client: client})
		},
	},
	"session": {
		typ:  reflect.TypeFor[SessionAbility](),
		name: "会话管理服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Session },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := NewHostServiceClient(conn)
			inject(impl, reflect.TypeFor[SessionAbility](), SessionClient{Client: client})
		},
	},
	"caller": {
		typ:  reflect.TypeFor[CallerAbility](),
		name: "插件调用服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Caller },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := NewHostServiceClient(conn)
			inject(impl, reflect.TypeFor[CallerAbility](), CallerClient{Client: client})
		},
	},
	"config": {
		typ:  reflect.TypeFor[ConfigAbility[any]](),
		name: "配置管理服务",
		id:   func(request *InjectAbility_Request) uint32 { return request.Config },
		apply: func(impl Plugin, conn *grpc.ClientConn) {
			client := NewHostServiceClient(conn)
			injectConfigSave(impl, func(pluginName string, data []byte) error {
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
		},
	},
}

func (s *server) GetAbilities(ctx context.Context, request *GetAbilities_Request) (*GetAbilities_Response, error) {
	s.abilities = []string{}

	for key, binding := range abilityBindings {
		if key == "config" && findConfigField(s.impl).IsValid() {
			s.abilities = append(s.abilities, key)
			continue
		}
		if key != "config" && check(s.impl, binding.typ) {
			s.abilities = append(s.abilities, key)
		}
	}

	return &GetAbilities_Response{Values: s.abilities}, nil
}

func emptyAbilityMessage(name string) string {
	return fmt.Sprintf("[plugin wrapper] 宿主%s地址为空", name)
}

func dialAbilityMessage(name string) string {
	return fmt.Sprintf("[plugin wrapper] 连接宿主%s失败", name)
}

func (s *server) injectAbility(binding abilityBinding, request *InjectAbility_Request) (*InjectAbility_Response, error) {
	id := binding.id(request)
	if id == 0 {
		message := emptyAbilityMessage(binding.name)
		slog.Error(message)
		return &InjectAbility_Response{Value: message}, errors.New(message)
	}

	conn, err := s.broker.Dial(id)
	if err != nil {
		slog.Error(dialAbilityMessage(binding.name), "err", err)
		return &InjectAbility_Response{Value: err.Error()}, err
	}

	binding.apply(s.impl, conn)
	return nil, nil
}

func (s *server) InjectAbilities(ctx context.Context, request *InjectAbility_Request) (*InjectAbility_Response, error) {
	// 判断插件是否嵌入能力
	for _, ability := range s.abilities {
		binding, ok := abilityBindings[ability]
		if !ok {
			continue
		}
		if resp, err := s.injectAbility(binding, request); err != nil {
			return resp, err
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
	data, err := toml.Marshal(field.Interface())
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
	if err := toml.Unmarshal(req.Data, field.Addr().Interface()); err != nil {
		return &SetConfig_Response{Result: false}, err
	}
	return &SetConfig_Response{Result: true}, nil
}
