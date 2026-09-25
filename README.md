# ASR Studio

Windowsでマイク音声を文字起こしし、OpenAI・Azure・Googleの結果を並べて比較するアプリです。[asr_test_docker](https://github.com/iuill/asr_test_docker) のクラウド音声認識機能を Go + Wails に移植しました。ローカルモデルやDockerは不要です。

## 使い始める

1. [Releases](https://github.com/iuill/asr_test_go/releases) から `asr_test_go-windows-x64.zip` をダウンロードして展開します。ZIPにはEXEと設定テンプレートが入っています。
2. `asr_test_go.exe` と同じフォルダの `appsettings.json` に、使うサービスの認証情報を設定します。設定項目は[設定例](appsettings.example.json)を参照してください。
3. EXEを起動し、入力マイクとモデルを選んで「録音を開始」を押します。設定ファイルを起動後に編集した場合は「設定を再読込」を押してください。

Windows 11、WebView2 Runtime、マイク権限、インターネット接続が必要です。クラウドAPIの利用料金が発生します。認証情報が空のモデルはグレー表示され、足りない項目がモデルの下に表示されます。

## 認証情報の設定

| サービス | `appsettings.json` に設定する項目 |
| --- | --- |
| OpenAI | `openai.api_key`。通常は `openai.base_url` を変更する必要はありません。 |
| Azure AI Speech | `azure_speech.api_key` とリソースの `azure_speech.endpoint`。 |
| Google Speech-to-Text | `google.project_id`、`google.location`、`google.service_account`。 |

`language` には `ja-JP` のような言語・地域コードを1つ設定します。Googleの `service_account` には、ダウンロードしたサービスアカウントJSONの**中身をJSONオブジェクトとして**貼り付けます。`credentials` フォルダにファイルを置くだけでは読み込まれません。GoogleではSpeech-to-Text APIの有効化、課金設定、必要なIAM権限も確認してください。Chirp 3の `location` は `us` または `eu` です。

`appsettings.json` はGitの管理対象から除外しています。認証情報を入れたファイルやZIPは公開しないでください。

## モデルと結果の表示

| モデル | 結果が表示されるタイミング |
| --- | --- |
| GPT Live Transcribe | 発話中に途中結果を表示。発話の区切りで確定します。 |
| GPT Transcribe | 発話後に送信し、確定結果を表示します。 |
| Google Speech-to-Text V1 / Chirp 3 | 録音中に音声を送り、APIから届いた途中結果と確定結果を表示します。短い発話では途中結果が出ない場合があります。 |
| Azure AI Speech / 話者識別 | 発話後に送信し、確定結果を表示します。 |

途中結果は後から変わるため画面だけに表示し、確定結果を履歴と自動保存に加えます。GPT TranscribeとAzureは約0.8秒の無音か15秒の発話で音声を区切ります。Googleは録音中ストリームを維持し、約4分ごとに切り替えます。GPT Liveは長時間録音時に50分ごとに接続を更新します。

## 録音と保存

- 入力マイクは一覧から選べます。名前が表示されない場合は「マイク一覧を更新」を押してマイク権限を許可してください。
- 遠くの声が抜ける場合は「離れた声を拾う」を録音開始前にONにできます。小さい音にも反応しますが、周囲の雑音やスピーカー音も拾いやすくなります。
- 「文字起こしに時刻を表示」は画面とテキスト保存に反映されます。自動保存中に切り替えた場合、その後に追加する行から反映されます。
- 「文字起こし結果を自動保存」をONにすると、録音ごとに `transcripts/recording-日時-ランダム文字列/` を作り、モデル別の `google-v1.txt` などへ確定結果を1件ずつ追記します。録音中にONにした場合は、その録音の既存結果も先に書き込みます。「結果を消去」は画面表示だけを消し、自動保存済みの結果には影響しません。
- 「デバッグログを保存」をONにすると `logs/asr-studio.log` に動作状況とAPIエラーを記録します。アプリ起動ごとに `APP START` 行を出し、ログを途中でONにした場合は `DEBUG LOGGING ENABLED` 行を出します。エラー応答は認証情報などを伏せ、1件4KBまでに制限します。APIが返したプロジェクトIDなどの診断情報は含まれます。

設定のON/OFFとマイク選択は次回起動時にも引き継がれます。自動保存とログの利用にはEXEを置いたフォルダへの書き込み権限が必要です。

## 開発・ビルド

Dev ContainerにはGo 1.27.1、Node.js 26、Wails CLI v2.16.0、MinGWを用意しています。

```bash
npm ci --prefix frontend
npm test --prefix frontend
npm run build --prefix frontend
go test ./...
./scripts/build-windows.sh
```

Windows版の出力は `build/bin/asr_test_go.exe` です。Goの公開メソッドを変更した場合は `./scripts/generate-bindings.sh` でフロントエンドのバインディングを更新してください。

[Windows release workflow](.github/workflows/release.yml) は `v0.0.1` のような `v*` タグをpushしたときにReleaseを作成し、EXEとZIPを添付します。PRのマージだけでは起動しません。Actions画面から手動実行した場合はArtifactのみ作成します。
