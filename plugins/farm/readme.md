# 农场小游戏插件

参照 `t-doc/FarmHandler.java` 移植的 Golem 农场插件。

## 功能

群聊小游戏，支持种植、收菜、偷菜、浇水、购买种子/土地/守卫、查询等级等。

## 部署方式

### 1. 编译插件

方式一（推荐，与项目示例一致）：

```bash
cd plugins/farm
go build
```

方式二（显式指定输出文件名）：

```bash
cd plugins/farm
go build -o farm.exe .
```

module 名为 `golem_plugin_farm`，因此方式一默认生成 `golem_plugin_farm.exe`；方式二适合希望产物直接叫 `farm.exe` 时使用。

### 2. 放到 Host 插件目录

将编译产物和 `农场图片/` 文件夹一起复制到 Host 的插件目录。以默认产物为例：

```text
golem_plugin_farm.exe
农场图片/
```

例如：

```text
host/plugins/
├── golem_plugin_farm.exe
├── 农场图片/
│   ├── 菜单_农场.jpg
│   ├── 植物_未耕.jpg
│   ├── 植物_已耕.jpg
│   └── 植物一_1.png ~ 植物一_5.png
```

### 3. 加载插件

在群里发送：

```text
/pm reload farm
```

## 数据文件

游戏数据默认保存在插件二进制同级目录下：

```text
data/farm_game.json
```

可以在配置文件中修改 `data_file` 路径。

## 图片加载说明

当前版本（默认）图片以**外部文件**形式存在，插件运行时从 `农场图片/` 目录读取。

如果需要把图片打包进单个二进制文件，可以使用 `//go:embed` 将图片嵌入，然后修改 `sendImageIfAvailable` 从嵌入资源读取。当前未默认启用此方式，因为外部图片便于替换和调试。

## 命令列表

| 命令 | 说明 |
|---|---|
| `农场` | 显示菜单 |
| `农场帮助` | 游戏规则说明 |
| `农场商店` | 查看可购买作物 |
| `守卫商店` | 查看守卫 |
| `农场购买土豆` / `农场购买土豆10` | 购买种子/守卫 |
| `查询土豆` / `查询斗牛犬` | 查询物品收益 |
| `种土豆` | 种植作物 |
| `收菜` | 收取成熟作物 |
| `偷菜@某人` | 偷取他人作物 |
| `浇水` / `浇水@某人` | 给自己/他人浇水 |
| `我的农场` | 查看资产与土地状态 |
| `农场等级` | 查看等级与升级所需经验 |
| `购买土地` | 扩展土地 |

## 配置文件

插件启动后会在 Host 配置中生成如下配置项：

```toml
[farm]
data_file = "data/farm_game.json"
image_dir = "农场图片"
initial_coins = 3000
initial_fields = 1
```

- `data_file`: 游戏数据文件路径，支持相对插件二进制目录的路径。
- `image_dir`: 图片目录，支持相对插件二进制目录的路径。
- `initial_coins`: 新玩家初始阳光。
- `initial_fields`: 新玩家初始土地数量。
