# A1O Platform 前端开发

| 应用 | 源码目录 | 部署路径 | 构建产物 |
| --- | --- | --- | --- |
| Studio | `web/studio` | `/` | `web/build/studio` |
| 管理控制台 | `web/admin` | `/admin/` | `web/build/admin` |

本地开发时，在仓库根目录执行以下命令安装两个应用的依赖。

```sh
npm ci --legacy-peer-deps --prefix web/studio
npm ci --legacy-peer-deps --prefix web/admin
```

先将 Go 后端启动在 3002 端口，再分别启动要开发的应用。两个 Vite 服务都将 `/api` 和 `/v1` 代理到 3002。Studio 也会代理 `/v1beta`，用于开发和验证 Gemini 接口。

```sh
npm start --prefix web/studio
npm start --prefix web/admin
```

默认从 <http://localhost:3000/> 打开 Studio，从 <http://localhost:3001/admin/> 打开管理控制台。在不同终端启动需要同时运行的服务。

生产构建会先执行类型检查。构建两个应用后再编译 Go 程序，使其嵌入最新前端资源。完整命令见[根目录 README](../README.zh-CN.md#从源码构建)。
