# asr_test_go

Go + Wails v2 + TypeScript の Windows アプリ用ひな形です。現在の画面と `Greet` は
Wails 標準テンプレートのサンプルで、ASR 機能はまだありません。

## 開発環境

Docker、Dev Container CLI、共通の [`dc`](https://github.com/iuill/devcontainer-user-env)
を用意し、このチェックアウトのルートから起動します。

```bash
dc bash
dc codex
```

Dev Container は Go 1.27.1、Node.js 26、TypeScript 7、Wails CLI v2.16.0、
Windows x64 向け MinGW と Linux 側の Wails 開発ライブラリを使います。
TypeScript は `frontend/package.json` に固定し、
`npm ci` で lockfile からインストールします。イメージの設定を変更した場合は
`dc rebuild` で再構築します。

Codex CLI 本体はホストの `~/.codex/packages/standalone` から読み取り専用で共有します。
`~/.codex/auth.json` と GitHub CLI の `~/.config/gh` は、ログイン状態を共有するため
読み書き可能な bind mount です。起動前にホストで `codex login` と `gh auth login`
を済ませてください。認証情報はイメージや Git リポジトリにコピーしません。

## Windows 向けビルド

```bash
dc ./scripts/build-windows.sh
```

出力は `build/bin/asr_test_go.exe` です。Linux コンテナからのクロスビルドでは
Wails の bindings 生成をスキップするため、生成済みの `frontend/wailsjs` を
Git で管理します。Go の公開メソッドを変えた際は、Linux 側で bindings を
再生成してから Windows ビルドを実行します。

```bash
dc ./scripts/generate-bindings.sh
dc ./scripts/build-windows.sh
```

ビルドは WebView2 Runtime を EXE に同梱しません。実行する Windows PC に
WebView2 Runtime が必要です。Windows 上での実行確認は別途行ってください。

フロントエンドだけを確認する場合:

```bash
dc npm ci --prefix frontend
dc npm run build --prefix frontend
```
