# objectstore

Go 对象存储抽象：一套 `Backend` 接口同时支持**本地卷**、**S3 兼容服务**（AWS S3 / Cloudflare R2 /
MinIO / RustFS 等）与**进程内内存**三种后端。业务代码只依赖对象键（object key），不碰文件路径、
签名 URL 或 SDK 调用；换后端只改配置，不改代码。

适合「容器里要持久化、换机器不能丢数据」的产物存储场景：抓取/转码/缩略图之类的输出，
写成对象键即可，本地开发用目录、生产用 S3 兼容服务。

## 安装

```bash
go get github.com/NoelOrin/objectstore
```

要求 Go 1.27+（S3 后端依赖 AWS SDK v2）。

## 快速开始

```go
import (
    "context"
    "time"

    "github.com/NoelOrin/objectstore"
)

func newStore() (objectstore.Backend, error) {
    backend, err := objectstore.Open(objectstore.Config{
        Driver:     "s3", // "" / "local" / "s3" / "memory"
        S3Endpoint: os.Getenv("S3_ENDPOINT"),
        S3Bucket:   os.Getenv("S3_BUCKET"),
        S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
        S3SecretKey: os.Getenv("S3_SECRET_KEY"),
        S3Prefix:   "app/prod",
    })
    if err != nil {
        return nil, err
    }
    // 启动即探活：配置错了马上失败，而不是等第一个请求。
    if err := backend.TestConnection(context.Background()); err != nil {
        return nil, err
    }
    return backend, nil
}

func saveAndRead(ctx context.Context, backend objectstore.Backend, data []byte) ([]byte, string, error) {
    if err := backend.Put(ctx, "metadata/abc/lyrics.lrc", data, "text/plain; charset=utf-8"); err != nil {
        return nil, "", err
    }
    out, mime, err := backend.Get(ctx, "metadata/abc/lyrics.lrc")
    return out, mime, err
}
```

## Backend 接口

| 方法 | 行为 |
| --- | --- |
| `Put(ctx, key, data, contentType)` | 覆盖写。`local` 先写同目录临时文件再 `rename`，不会留下半写文件 |
| `Get(ctx, key)` | 返回 `([]byte, contentType, error)`；对象不存在时 `errors.Is(err, objectstore.ErrNotFound)` |
| `Delete(ctx, key)` | 删除对象；**删不存在的对象不算错误**（幂等） |
| `Stat(ctx, key)` | 返回字节数与修改时间（Unix 毫秒） |
| `List(ctx, prefix)` | 前缀下的对象键，**字典序**；S3 自动翻页 |
| `TestConnection(ctx)` | 探活：`local` 写一个探针文件，`s3` 走 `HeadBucket` |
| `Name()` | 返回当前后端名（`local` / `s3` / `memory`），便于日志 |

键统一用正斜杠（`a/b/c.jpg`）；各后端自行映射到本地路径或 S3 object key。

## 配置

| 字段 | 说明 |
| --- | --- |
| `Driver` | `""`/`local`（默认）、`s3`、`memory`；未知值直接报错 |
| `LocalDir` | 本地根目录，默认 `./metadata` |
| `S3Endpoint` / `S3Bucket` | S3 后端必填 |
| `S3Region` | 默认 `us-east-1` |
| `S3AccessKey` / `S3SecretKey` | 静态凭证；留空走 SDK 默认凭证链 |
| `S3Prefix` | 统一对象键前缀（如 `app/prod`），读写自动拼接 |
| `S3ForcePathStyle` | `true` 时强制 path-style；非 AWS 域名默认自动开启 |
| `PublicURL` / `S3PublicBaseURL` | 仅供调用方拼对外 URL，库本身不参与（服务端读写不经过公网） |

## 三种后端

- **local**：对象键先以 `/` 为根做 `filepath.Clean` 再拼根目录，`../` 无法逃出根目录（有专门测试）；写用临时文件 + `rename` 原子替换。适合单机与容器挂卷。
- **s3**：AWS SDK v2；任意 S3 兼容服务。非 AWS 域名自动启用 path-style；每次操作带 10s 超时；`S3Prefix` 自动拼到键前。适合需要跨实例共享的持久化。
- **memory**：进程内、重启即丢，只给测试用。

```go
// 等价于 local，读 ./metadata
b, _ := objectstore.Open(objectstore.Config{})
```

## 错误与语义

- 缺失对象只有一条明确信号：`errors.Is(err, objectstore.ErrNotFound)`（`Get`/`Stat`）。不要比字符串。
- `Delete` 幂等；`List` 不做分页参数，调用方按前缀自行收窄。
- `Open` 只构造不发网络请求；`TestConnection` 才是探活，建议启动时调用一次。

## 测试

```bash
go test ./...
```

`local` 与 `memory` 后端默认全跑（共享同一套 `Backend` 契约）。真实 S3 兼容服务的契约测试靠环境变量开启：

| 变量 | 必需 | 说明 |
| --- | --- | --- |
| `OBJECTSTORE_TEST_S3_ENDPOINT` | 是 | 如 `https://accountid.r2.cloudflarestorage.com` |
| `OBJECTSTORE_TEST_S3_BUCKET` | 是 | 测试桶 |
| `OBJECTSTORE_TEST_S3_REGION` | 否 | 默认 `us-east-1` |
| `OBJECTSTORE_TEST_S3_ACCESS_KEY` / `OBJECTSTORE_TEST_S3_SECRET_KEY` | 否 | 留空走默认凭证链 |

未设置时该用例显式 `t.Skip("no S3 test credentials")`，不会静默通过。旧名 `MUSIC_TEST_S3_*` 仍然兼容。
