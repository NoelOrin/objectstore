# libs/go/objectstore

Go 服务共用的对象存储抽象，设计参照 GOSpeak `app/server/internal/storage`：
一套 `Backend` 接口，屏蔽本地卷、S3 兼容服务与进程内内存三种后端。业务代码只依赖对象键，
不碰文件路径或 SDK 调用；换后端只是改配置，不改代码。

## 用法

```go
backend, err := objectstore.Open(objectstore.Config{
    Driver: "s3", // "" / "local" / "s3" / "memory"
    S3Endpoint: os.Getenv("S3_ENDPOINT"),
    S3Bucket:   os.Getenv("S3_BUCKET"),
    S3Prefix:   "music/prod",
})
if err != nil { return err }
if err := backend.TestConnection(ctx); err != nil { return err } // 启动即探活

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := backend.Put(ctx, "metadata/abc/lyrics.lrc", data, "text/plain; charset=utf-8"); err != nil { return err }
obj, mime, err := backend.Get(ctx, "metadata/abc/lyrics.lrc")
_, _ = obj, mime
```

`projects/music` 用它持久化刮削出来的歌词与封面（容器里换卷不丢数据，或直接落 S3 / R2 / MinIO）。

## Backend

| 方法 | 说明 |
| --- | --- |
| `Put(ctx, key, data, contentType)` | 覆盖写；`Local` 用同目录临时文件 + rename 做原子写 |
| `Get(ctx, key)` | 返回字节与 MIME，缺失时 `errors.Is(err, objectstore.ErrNotFound)` |
| `Delete(ctx, key)` | 删除；删不存在的对象不算错误 |
| `Stat(ctx, key)` | 字节数与修改时间（Unix 毫秒） |
| `List(ctx, prefix)` | 前缀下的键，字典序；S3 自动翻页 |
| `TestConnection(ctx)` | `Local` 写探针文件；S3 走 `HeadBucket` |
| `Name()` | `local` / `s3` / `memory`，便于日志 |

## 后端

- **local**：默认根目录 `./metadata`。键先以 `/` 为根做 `filepath.Clean` 再拼根目录，
  杜绝 `../` 穿越；写入用临时文件 + rename，避免半写文件。
- **s3**：AWS SDK v2。非 AWS 域名或显式 `S3ForcePathStyle` 自动启用 path-style；
  `S3Region` 默认 `us-east-1`；静态凭证，所有操作带 10s 超时。
- **memory**：测试用，重启即丢。

`objectstore.Open(Config{Driver: ""})` 等价于 `local`。

```bash
go test ./...
```

设置 `MUSIC_TEST_S3_ENDPOINT` + `MUSIC_TEST_S3_BUCKET`（可选 `*_REGION` / `*_ACCESS_KEY` /
`*_SECRET_KEY`）时会额外跑一遍真实 S3 兼容服务的契约测试；未配置则 `t.Skip`。
