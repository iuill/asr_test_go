# ASR Studio（asr_test_go）

[asr_test_docker](https://github.com/iuill/asr_test_docker) のクラウド音声認識機能を Go + Wails v2 の Windows アプリに移したものです。マイクから発話を取り込み、選択した複数のモデルの結果を並べて比較できます。入力マイクは一覧から選択でき、録音中は実際に使用しているデバイス名を表示します。GPT Liveは発話中に途中結果を表示します。その他のモデルは発話終了から約0.8秒の無音で送信し、長い発話は15秒で区切ります。結果はテキストとして保存できます。

GPT LiveはRealtime APIでストリーミングし、その他のモデルは発話単位の同期APIを使います。閲覧者向けセッション共有とログイン機能は含まれません。ローカルモデルやDockerは不要です。録音開始時にモデル一覧は自動で折りたたまれ、マイク入力のスペクトルを表示します。

## 対応API

| プロバイダー | モデル/API |
| --- | --- |
| OpenAI | `gpt-transcribe` / Audio Transcriptions API、`gpt-live-transcribe` / Realtime API |
| Azure AI Speech | Fast Transcription REST API `2025-10-15`、話者識別オプション |
| Google Cloud Speech-to-Text | V1、V2 `chirp_3` |

APIの利用料金が発生します。インターネット接続と、利用するAPIの契約・有効化が必要です。Google Chirp 3 は `us` または `eu` のロケーションを使用します。

## Windowsで使う

GitHubの **Releases** から `asr_test_go-windows-x64.zip` をダウンロードして展開します。EXE単体もReleaseに添付されますが、ZIPには設定ファイルが含まれています。`asr_test_go.exe` と同じフォルダの `appsettings.json` を編集し、利用するサービスの認証情報を設定してください。アプリを開いて「設定を再読込」を押すと反映されます。空欄のサービスはモデル一覧で選択できず、モデルの下に不足する設定項目が表示されます。配布時の設定ファイルは認証情報が空なので、初回起動時はすべてのモデルが選択できません。

設定例は [appsettings.example.json](appsettings.example.json) にあります。

```json
{
  "language": "ja-JP",
  "openai": {
    "api_key": "sk-...",
    "base_url": "https://api.openai.com/v1"
  },
  "azure_speech": {
    "api_key": "...",
    "endpoint": "https://YOUR-RESOURCE.cognitiveservices.azure.com"
  },
  "google": {
    "project_id": "your-project-id",
    "location": "us",
    "service_account": {
      "type": "service_account",
      "project_id": "your-project-id",
      "private_key_id": "...",
      "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
      "client_email": "...@....iam.gserviceaccount.com",
      "client_id": "...",
      "token_uri": "https://oauth2.googleapis.com/token"
    }
  }
}
```

Googleの `service_account` には、ダウンロードしたサービスアカウントJSON全体をオブジェクトとして貼り付けます。Speech-to-Text APIの権限と課金設定が必要です。`appsettings.json` はGit管理対象から除外しています。キーが入ったZIPを他人に渡さないでください。

Windows 11とWebView2 Runtimeが必要です。実機のマイク権限を許可してください。

マイクの名前が表示されない場合は「マイク一覧を更新」を押し、マイク使用を許可してください。選んだマイクは次回起動時にも復元されます。デバイスが取り外された場合はシステム既定に戻ります。`gpt-live-transcribe` は24 kHz PCMを発話中に送信し、発話が区切られるたびに確定結果を受け取ります。OpenAIの古い `gpt-4o-transcribe` 系は[2027年2月26日に廃止予定](https://developers.openai.com/api/docs/deprecations)のため、モデル一覧に含めていません。

全指向性マイクなどで離れた声が抜ける場合は、入力マイクの「離れた声を拾う」を ON にして録音を開始できます。発話判定を小さい音にも反応する設定にし、発話直前の約0.3秒も含めて送信します。ブラウザのノイズ抑制とエコー除去も無効化するため、周囲の雑音やスピーカーの音も拾いやすくなります。切替はこのPCに記憶され、録音中は変更できません。機器や席の配置によって改善度は異なります。

GPT LiveのRealtime接続はセッションの60分上限より前に、録音を続けたまま50分ごとに更新します。更新時は現在の発話を区切って確定させます。長時間の利用中は、API接続と自動保存のエラー表示を確認してください。

`language` は必須です。`ja-JP` のような言語・地域コードを 1 つ指定してください。OpenAI には言語部分 (`ja`)、Azure と Google には `ja-JP` を送ります。`azure_speech.locale` と `google.locale` は使用しません。既存の設定ファイルから削除し、ルートの `language` に移してください。スペクトル表示は操作ボタンの「停止」と「結果を消去」の間にあります。

画面の「デバッグログを保存」を ON にすると、EXE と同じフォルダの `logs/asr-studio.log` に動作状況とエラー種別を記録します。各ファイルは最大 5MB で、現在のログと `.1`、`.2` の計 3 世代を保持します。OFF の間はログを書きません。選択状態は EXE 横の `asr-studio.preferences.json` に保存され、次回起動時にも引き継がれます。API キー、音声、文字起こし本文はログに記録しません。EXE を置いたフォルダに書き込み権限が必要です。

画面の「文字起こしに時刻を表示」で、各確定結果の先頭にローカル時刻 `[hh:mm]` を付けるか切り替えられます。テキスト保存にも現在の表示設定が反映され、選択状態は同じ `asr-studio.preferences.json` に保存されます。新着結果のカードは短く光り、結果欄が末尾を表示している場合は自動で下へスクロールします。

画面の「文字起こし結果を自動保存」を ON にすると、録音ごとに EXE 横の `transcripts` フォルダへ日時付きのテキストファイルを作り、確定した文字起こしが追加されるたびと録音停止時に内容を更新します。この切替も `asr-studio.preferences.json` に保存されます。途中の聞き取り結果は保存しません。「結果を消去」で画面を空にしても、録音中の自動保存ファイルに蓄積した結果は消しません。保存エラーは画面に表示します。

## 開発・ビルド

Dev Containerでは Go 1.27.1、Node.js 26、Wails CLI v2.16.0、MinGW を使用します。

```bash
npm ci --prefix frontend
./scripts/generate-bindings.sh
npm run build --prefix frontend
npm test --prefix frontend
go test ./...
./scripts/build-windows.sh
```

出力は `build/bin/asr_test_go.exe` です。初回ビルド時には同じフォルダに設定テンプレートもコピーします。生成済みの `frontend/wailsjs` をGitで管理するため、Goの公開メソッドを変更したら `generate-bindings.sh` を再実行してください。

設定の読込・検証は `config.go`、クラウドへの発話送信は `transcription.go`、Live接続は `live.go` に分けています。ファイル保存は `persistence.go` に集約しています。フロントエンドの音声変換、履歴管理、自動保存の順序制御はそれぞれ `audio-encoding.ts`、`transcript-store.ts`、`autosave-session.ts` に置き、画面や実際のAPI接続なしでテストできます。録音停止時は、自動保存のON/OFFに関わらず処理中の認識を待ってから次の録音を受け付けます。

## GitHub Releases

`v0.0.1` のような `v` で始まるタグをpushすると、[Windows release workflow](.github/workflows/release.yml) がWindows上で検証・ビルドし、EXEと空の設定ファイルを入れたZIPをGitHub Releaseに添付します。アプリ画面右上に埋め込みバージョンが表示されます。Actions画面から手動実行した場合はActions Artifactのみ作成し、バージョンは `0.0.1` です。
