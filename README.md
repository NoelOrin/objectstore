# objectstore

> 语言：[简体中文](README.md) · [English](README.en.md)

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
    "os"

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

## 可选能力：Presigner

`Backend` 之外的能力通过可选接口暴露，不影响已有实现：

```go
type Presigner interface {
    PresignUpload(ctx context.Context, key, contentType string, size int64, expires time.Duration) (*PresignedUpload, error)
}
```

目前只有 `s3` 后端实现；用 `backend.(objectstore.Presigner)` 判断。返回的 URL 是带签名的 PUT 请求，`size > 0` 时 `ContentLength` 会进签名头，多传/少传字节都会被服务端拒绝。`expires <= 0` 时默认 5 分钟。local 与 memory 不实现此接口，调用方据此决定是否走服务端中转。

另一个可选接口 `PublicURLer` 由 local 与 s3 实现，按 `Config.PublicURL` / `S3PublicBaseURL` 返回公开访问 URL；S3 未配置基址时自动按 AWS 虚拟主机或 path-style 拼接。调用方用 `backend.(objectstore.PublicURLer)` 判断，不需要自己拼 URL。

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
| `PublicURL` | local 后端 `PublicURL()` 的公开基址（如 `/uploads`） |
| `S3PublicBaseURL` | S3 后端 `PublicURL()` 的公开基址（如 CDN 域名），优先于自动拼接 |
| `S3Timeout` | 单次操作超时，默认 10s |
| `S3PresignTTL` | `PresignUpload` 默认有效期，默认 5m |

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
