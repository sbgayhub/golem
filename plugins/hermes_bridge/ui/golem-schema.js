// Golem 插件设置台的字段表（schema）
//
// 数据来源：各插件 plugins/<name>/ 里的 Config struct（含 toml/comment tag）、
// 本仓库 plugins/config.toml 的实际键名，以及各插件 readme。
// 已知 readme 与代码不一致时以代码为准：
//   - ai readme 写 1.1.0，代码是 1.2.0；
//   - profile readme 写 1.1.0，代码是 1.1.1；
//   - cron readme 提「6 段」，代码用 robfig/cron 标准 5 段；
//   - universal 规则是 10 个字段（不是 11）。
//
// version 只是「这份表单是照哪个版本的代码写的」的标识，不参与任何校验。
(() => {
  // 所有插件共用的公共段（host 的 plugin.Config）。
  const COMMON = [
    {
      key: "enable",
      label: "启用",
      type: "bool",
      help: "关掉后 host 不再向该插件派发消息（进程仍在、OnLoad 已执行）。改动立即生效，不需重启。",
    },
    {
      key: "mode",
      label: "权限模式",
      type: "enum",
      options: [
        ["blacklist", "黑名单（limits 内禁用）"],
        ["whitelist", "白名单（仅 limits 内可用）"],
      ],
      help: "其它取值一律放行；机器人主人始终放行。",
    },
    {
      key: "limits",
      label: "会话列表",
      type: "recipients",
      multi: true,
      help: "群聊填群 ID（形如 xxx@chatroom），私聊填对方 wxid。按上面的模式被禁用或只在这几个会话里可用。",
    },
  ];

  // 所有插件都能挂的「会话列表」来源：/admin/data/recipients 的缓存全量。
  const RECIPIENTS_SOURCE = "recipients";

  const SCHEMAS = {
    ai: {
      version: "1.2.0",
      title: "ai · 大模型对话",
      desc: "接 OpenAI 兼容接口，在群里按概率接话；provider 与提示词都可以按会话覆盖。",
      groups: [
        {
          title: "全局行为",
          fields: [
            {
              key: "config.active_provider",
              label: "当前 provider",
              type: "text",
              help: "取值必须是下面 providers 里的某个名字。",
            },
            {
              key: "config.active_prompt",
              label: "当前提示词",
              type: "text",
              help: "取值必须是 prompts 里的某个名字。",
            },
            {
              key: "config.reply_rate",
              label: "群聊回复概率",
              type: "number",
              min: 0,
              max: 1,
              step: 0.01,
              help: "0~1。0 完全不接话，1 每条都回；私聊与 @/引用始终回复。",
            },
            {
              key: "config.max_context_messages",
              label: "上下文消息数",
              type: "int",
              min: 0,
              help: "每个会话最多保留多少条对话历史。",
            },
            {
              key: "config.http_timeout_seconds",
              label: "请求超时",
              type: "int",
              min: 0,
              unit: "秒",
              help: "大模型请求的缺省超时；provider 里可单独覆盖。",
            },
            {
              key: "config.silence",
              label: "全局静默",
              type: "bool",
              help: "开启后只回 @ 与引用，不再按概率接话。",
            },
          ],
        },
      ],
      collections: [
        {
          kind: "map-of-table",
          key: "config.providers",
          title: "Provider 预设",
          labelField: null,
          help: "每个 provider 是一组 OpenAI 兼容接口参数；改名字需要手工编辑原文。",
          fields: [
            {
              key: "base_url",
              label: "接口地址",
              type: "url",
              help: "形如 https://api.openai.com/v1。",
            },
            { key: "api_key", label: "密钥", type: "password" },
            { key: "model", label: "模型名", type: "text" },
            {
              key: "http_timeout_seconds",
              label: "超时",
              type: "int",
              unit: "秒",
              help: "0 表示用全局缺省。",
            },
          ],
        },
        {
          kind: "map-of-scalar",
          key: "config.prompts",
          title: "提示词",
          help: "名字 → 提示词正文。名字里的特殊字符会以 [\"…\"] 形式出现，可正常编辑正文。",
          valueType: "textarea",
        },
        {
          kind: "map-of-table",
          key: "config.session_configs",
          title: "会话级覆盖",
          help: "按会话覆盖上面的全局设置；没写的字段沿用全局。会话键形如 chatroom:xxx@chatroom 或 private:wxid。",
          fields: [
            {
              key: "active_provider",
              label: "provider",
              type: "tri-text",
            },
            { key: "active_prompt", label: "提示词", type: "tri-text" },
            {
              key: "reply_rate",
              label: "回复概率",
              type: "tri-number",
              min: 0,
              max: 1,
              step: 0.01,
            },
            {
              key: "max_context_messages",
              label: "上下文消息数",
              type: "tri-int",
              min: 0,
            },
            { key: "silence", label: "静默", type: "tri-bool" },
          ],
        },
      ],
    },

    universal: {
      version: "1.0.0",
      title: "universal · 关键词调接口",
      desc: "消息第一段命中关键词时请求一个 HTTP 接口，把结果发回会话。",
      groups: [
        {
          title: "全局",
          fields: [
            {
              key: "config.http_timeout_seconds",
              label: "请求超时",
              type: "int",
              min: 0,
              unit: "秒",
            },
          ],
        },
      ],
      collections: [
        {
          kind: "array-of-table",
          key: "config.rules",
          title: "规则",
          labelField: "id",
          help: "本页只能改已有规则的字段，增删规则请用原文编辑。",
          fields: [
            { key: "id", label: "规则 ID", type: "text", help: "仅作标识。" },
            {
              key: "keywords",
              label: "关键词",
              type: "tags",
              help: "消息第一段全文匹配其中一个即触发。",
            },
            { key: "url", label: "请求地址", type: "url", help: "支持模板变量。" },
            {
              key: "method",
              label: "请求方法",
              type: "enum",
              options: [
                ["GET", "GET"],
                ["POST", "POST"],
              ],
            },
            {
              key: "headers",
              label: "请求头",
              type: "text",
              help: "格式 A=B;C=D，支持模板变量。",
            },
            { key: "body", label: "请求体", type: "textarea" },
            {
              key: "send_type",
              label: "发送类型",
              type: "enum",
              options: [
                ["text", "文本"],
                ["emoji", "表情"],
                ["image", "图片"],
                ["video", "视频"],
              ],
            },
            {
              key: "result_path",
              label: "取值路径",
              type: "text",
              help: "gjson 路径，例如 data.url；留空则用响应原文。",
            },
            {
              key: "at",
              label: "@ 对方",
              type: "bool",
              help: "文本回复时是否 @ 或引用触发者。",
            },
            {
              key: "enabled",
              label: "启用本条",
              type: "tri-bool",
              help: "键缺失时按启用处理。",
            },
          ],
        },
      ],
    },

    cron: {
      version: "0.0.1",
      title: "cron · 定时任务",
      desc: "按 cron 表达式定时调用其它插件的能力（如 ai.chat、text.to.image）。",
      groups: [],
      collections: [
        {
          kind: "array-of-table",
          key: "config.jobs",
          title: "任务",
          labelField: "cron",
          help: "表达式为标准 5 段（分 时 日 月 周）。新增任务请用原文编辑。",
          fields: [
            { key: "cron", label: "cron 表达式", type: "text", help: "如 0 9 * * *。" },
            {
              key: "target",
              label: "发送目标",
              type: "recipients",
              multi: true,
            },
            {
              key: "capability",
              label: "调用能力",
              type: "text",
              help: "形如 ai.chat / text.to.image；须是 host 已注册的能力。",
            },
          ],
        },
      ],
    },

    profile: {
      version: "1.1.1",
      title: "profile · 群成员画像",
      desc: "统计群聊历史消息，让大模型生成成员画像。",
      groups: [
        {
          title: "行为",
          fields: [
            {
              key: "config.chunk_token_budget",
              label: "分块 token 上限",
              type: "int",
              min: 1,
              help: "按内容量切块而非条数；中文模型可调大到 6000。",
            },
            {
              key: "config.max_single_msg_chars",
              label: "单条消息截断",
              type: "int",
              min: 1,
              unit: "字",
              help: "防止超长消息撑爆请求。",
            },
            {
              key: "config.cold_start_max_messages",
              label: "冷启动消息上限",
              type: "int",
              min: 1,
              unit: "条",
              help: "安全天花板，超出只取最近的部分。",
            },
            {
              key: "config.cold_start_max_chunks",
              label: "冷启动分块上限",
              type: "int",
              min: 1,
              unit: "块",
              help: "超出丢弃最早的分块。",
            },
            {
              key: "config.render_image",
              label: "画成图片",
              type: "bool",
              help: "开：经 gg 的 markdown.to.image 渲染成 PNG；关：发纯文本。gg 未启用或渲染失败会自动回退文本。",
            },
          ],
        },
      ],
    },

    setu: {
      version: "1.0.1",
      title: "setu · 随机图",
      desc: "按指令取随机图片/视频；接口返回的响应体就是图片或视频直链。",
      groups: [
        {
          title: "图片接口",
          fields: [
            { key: "config.img_url", label: "二次元", type: "url" },
            { key: "config.boy_url", label: "帅哥", type: "url" },
            { key: "config.heisi_url", label: "黑丝", type: "url" },
            { key: "config.baisi_url", label: "白丝", type: "url" },
            { key: "config.heisi_video_url", label: "黑丝视频", type: "url" },
            { key: "config.baisi_video_url", label: "白丝视频", type: "url" },
            {
              key: "config.search_url",
              label: "搜图接口前缀",
              type: "url",
              help: "关键词会 URL 编码后追加；默认接口的 id/key 需换成自己的。",
            },
          ],
        },
        {
          title: "行为",
          fields: [
            {
              key: "config.video_rate",
              label: "出视频概率",
              type: "int",
              min: 0,
              max: 100,
              unit: "%",
              help: "≥100 必出视频，≤0 永不出；视频失败会降级成图片。",
            },
          ],
        },
      ],
    },

    demos: {
      version: "1.1.0",
      title: "demos · 功能演示",
      desc: "一堆小功能（百科、语音、视频等）的演示插件。",
      groups: [
        {
          title: "行为",
          fields: [
            {
              key: "config.video_native",
              label: "视频原生上传",
              type: "bool",
              help: "走 CDN 原生上传；失败则回退到链接卡片。",
            },
            {
              key: "config.max_list",
              label: "列表条数上限",
              type: "int",
              min: 1,
            },
            {
              key: "config.bdbk_url",
              label: "百科接口",
              type: "url",
              help: "百度百科 API 地址，需自备 appid。",
            },
            {
              key: "config.silk_encoder_path",
              label: "silk 编码器路径",
              type: "path",
              help: "silk_v3_encoder.exe 的绝对路径；留空则语音转码回退 AMR。",
            },
            {
              key: "config.silk_sample_rate",
              label: "silk 采样率",
              type: "int",
              unit: "Hz",
              help: "仅当编码器路径非空时生效。",
            },
            {
              key: "config.silk_max_bytes",
              label: "单条语音字节预算",
              type: "int",
              unit: "字节",
              help: "超预算自动降码率、必要时裁剪时长；0 或负数关闭。",
            },
          ],
        },
      ],
    },

    farm: {
      version: "1.0.0",
      title: "farm · 农场游戏",
      desc: "群聊小游戏；数据落在本地 JSON 文件里。",
      groups: [
        {
          title: "数据与初始值",
          fields: [
            { key: "config.data_file", label: "数据文件", type: "path" },
            { key: "config.image_dir", label: "图片目录", type: "path" },
            {
              key: "config.initial_coins",
              label: "初始阳光",
              type: "int",
              min: 0,
              help: "新玩家的初始数量。",
            },
            {
              key: "config.initial_fields",
              label: "初始土地",
              type: "int",
              min: 0,
            },
          ],
        },
      ],
    },

    robbery: {
      version: "1.0.0",
      title: "robbery · 打劫游戏",
      desc: "群成员互抢的数值游戏。",
      groups: [
        {
          title: "数据与初始值",
          fields: [
            { key: "config.data_file", label: "数据文件", type: "path" },
            { key: "config.initial_money", label: "初始金钱", type: "int", min: 0 },
            {
              key: "config.initial_strength",
              label: "初始武力",
              type: "int",
              min: 0,
            },
            {
              key: "config.job_refresh_hours",
              label: "任务刷新间隔",
              type: "int",
              min: 0,
              unit: "小时",
            },
            {
              key: "config.welfare_amount",
              label: "破产救济金额",
              type: "int",
              min: 0,
            },
            {
              key: "config.welfare_cooldown",
              label: "救济冷却",
              type: "int",
              min: 0,
              unit: "小时",
            },
            {
              key: "config.change_prof_cost",
              label: "转职花费",
              type: "int",
              min: 0,
            },
          ],
        },
      ],
    },

    wordcloud: {
      version: "1.0.2",
      title: "wordcloud · 词云",
      desc: "统计群聊历史发言生成词云图片；历史消息经 statistics 插件取，需 statistics 启用。",
      groups: [
        {
          title: "出图",
          fields: [
            { key: "config.max_words", label: "最多词数", type: "int", min: 1 },
            { key: "config.width", label: "宽度", type: "int", min: 1, unit: "px" },
            { key: "config.height", label: "高度", type: "int", min: 1, unit: "px" },
            {
              key: "config.min_font_size",
              label: "最小字号",
              type: "number",
              min: 1,
            },
            {
              key: "config.max_font_size",
              label: "最大字号",
              type: "number",
              min: 1,
            },
            {
              key: "config.max_messages",
              label: "统计消息上限",
              type: "int",
              min: 1,
              unit: "条",
              help: "超出取最近的这么多条。",
            },
          ],
        },
        {
          title: "字体",
          fields: [
            {
              key: "config.font_path",
              label: "自定义字体",
              type: "path",
              help: "ttf/otf 绝对路径；留空用内置字体。",
            },
          ],
        },
      ],
    },

    meme: {
      version: "v1.0.0",
      title: "meme · 表情包生成",
      desc: "调外部表情包服务的本地地址。",
      groups: [
        {
          title: "服务",
          fields: [
            {
              key: "config.Url",
              label: "服务地址",
              type: "url",
              help: "键名就是大写开头的 Url（Go 的 toml 解码大小写不敏感）。",
            },
          ],
        },
      ],
    },

    // 以下 7 个没有 [x.config] 段，只有公共段。
    gg: { version: "v0.0.0", title: "gg · 图文渲染", desc: "markdown 转图片等通用能力。", groups: [] },
    music: { version: "1.0.0", title: "music · 点歌", desc: "音乐搜索与卡片发送。", groups: [] },
    news: { version: "v0.0.1", title: "news · 新闻", desc: "新闻聚合。", groups: [] },
    reread: { version: "v1.0.0", title: "reread · 回顾", desc: "聊天回顾。", groups: [] },
    statistics: {
      version: "1.3.0",
      title: "statistics · 消息统计",
      desc: "群消息统计，供 wordcloud / profile 取历史消息。",
      groups: [],
    },
    video_parser: {
      version: "v1.0.0",
      title: "video_parser · 视频解析",
      desc: "短视频链接解析。",
      groups: [],
    },
    fake_forward: {
      version: "2.0.0",
      title: "fake_forward · 假转发",
      desc: "伪造聊天记录卡片。",
      groups: [],
    },
  };

  // 插件列表的排列顺序：常用的在前。
  // hermes_bridge 自己不出现在本页（它有专门的管理台）；
  // 旧版 hermes 插件已弃用，本页不再列出（见 DEPRECATED_PLUGINS）。
  const PLUGIN_ORDER = [
    "ai", "universal", "cron", "profile", "setu",
    "wordcloud", "robbery", "farm", "demos", "meme",
    "gg", "music", "news", "reread", "statistics", "video_parser",
    "fake_forward",
  ];

  // 已弃用、本页完全不展示的插件：host 仍会读到它们的段，但不该引导用户去改。
  const DEPRECATED_PLUGINS = ["hermes"];

  window.GOLEM_SCHEMA = {
    COMMON,
    RECIPIENTS_SOURCE,
    SCHEMAS,
    PLUGIN_ORDER,
    DEPRECATED_PLUGINS,
  };
})();
