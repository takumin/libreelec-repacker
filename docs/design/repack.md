# repack コマンドの設計

状態：草案（実装はまだ承認されていません）

この文書は、公式 LibreELEC イメージを宣言ファイルに従って書き換え、新しいイメージとして出力する `repack` コマンドの設計をまとめたものです。
最初に実現する機能は、Kodi の言語設定の永続化です。

## 現状

実装済みのコマンドは、読み取り専用の `inspect` だけです。
`inspect` は、raw または gzip 圧縮のイメージを開き、パーティション表を読み、`SYSTEM` を含む FAT の boot パーティションを見つけて検証します。

関連するパッケージの役割は次のとおりです。

- **`internal/diskimage`**：ディスクイメージへの厳密に読み取り専用のアクセスを提供します。LibreELEC については何も知りません。
- **`internal/libreelec`**：LibreELEC イメージのレイアウトを発見して検証します。
- **`internal/command/inspect`**：`inspect` コマンドです。
- **`internal/config`**：ログレベルなど、アプリケーション自体の設定を Functional Option Pattern で保持します。

イメージを書き込む経路は、まだ一つもありません。

## 目標と非目標

目標は次のとおりです。

- 入力イメージを変更せずに、カスタマイズ済みのイメージを出力します。
- root 権限もマウントも使わずに動作します（`inspect` と同じ制約です）。
- 最初のユースケースとして、Kodi の表示言語を初回セットアップ後も保持させます。

当面の非目標は次のとおりです。

- `SYSTEM`（SquashFS）の中身の改変。
- ext4 パーティション（`/storage`）への直接の書き込み（理由は次節で述べます）。

## `/storage` に直接書き込めない理由

Kodi の言語設定は `/storage/.kodi/userdata/guisettings.xml` の `locale.language` に保存されます。
そのため、イメージの ext4 パーティションにこのファイルを置けば済むように見えます。
しかし、LibreELEC の初回起動処理がこの方法を無効にします。

公式イメージを作る `scripts/mkimage` は、ext4 パーティションを 32MB 程度で作り、`.please_resize_me` という印のファイルだけを入れます[^mkimage]。
初回起動時、systemd サービスの `fs-resize` がこの印を見つけると、パーティションをディスク末尾まで広げたうえで `mke2fs` によりファイルシステムを作り直します[^fs-resize]。
中身を残したまま広げる `resize2fs` ではないので、事前に置いたファイルはすべて消えます。

さらに `fs-resize` は、`/storage/.kodi`、`/storage/.config`、`/storage/.cache` のいずれかが存在すると「初期化済みのシステム」と判断し、リサイズ自体を中止します。
したがって ext4 にファイルを仕込むと、設定が消えるか、`/storage` が 32MB のまま残るかのどちらかになります。

[^mkimage]: <https://github.com/LibreELEC/LibreELEC.tv/blob/master/scripts/mkimage>（part2 の作成、`populatefs` による `.please_resize_me` の配置）
[^fs-resize]: <https://github.com/LibreELEC/LibreELEC.tv/blob/master/packages/sysutils/busybox/scripts/fs-resize>

## FAT 側のフックによる永続化

LibreELEC の initramfs の `init` は、起動の各段階で FAT パーティション（起動中は `/flash`）上のスクリプトを `.` で読み込みます[^init]。

- **`post-flash.sh`**：`/flash` をマウントした直後に読み込まれます。
- **`post-sysroot.sh`**：`SYSTEM` を `/sysroot` にマウントした直後に読み込まれます。
- **`mount-storage.sh`**：存在すると、`mount_storage` の中で通常の `mount_part "$disk" "/storage" "rw,noatime"` の代わりに読み込まれます。

起動段階の順序は `check_disks`、`mount_flash`、`cleanup_flash`、`update_bootmenu`、`load_splash`、`mount_sysroot`、`mount_storage`、`check_update`、`prepare_sysroot` です。
`cleanup_flash` が削除するのは Raspberry Pi の EEPROM 更新ファイルだけで、追加したスクリプトや種ファイルは消しません。
また、アップデート処理が `/flash` で置き換えるのは `KERNEL` と `SYSTEM`（とそれぞれの `.md5`）だけです。

`repack` は `mount-storage.sh` を使い、次の流れで種ファイルを `/storage` に一度だけコピーします。

1. 通常どおり `mount_part "$disk" "/storage" "rw,noatime"` で `/storage` をマウントします。
2. `/storage/.please_resize_me` が存在すれば、何もせずに終わります。初回起動では `fs-resize` がこのあと `/storage` を作り直すので、ここでコピーしても消えるうえ、`.kodi` があるとリサイズが中止されるからです。
3. 印がなく、適用済みマーカーもなければ、`/flash` 上の種ファイルのディレクトリを `/storage` へコピーし、マーカーを作ります。

結果として、初回起動でリサイズと再起動が行われ、二回目の起動で設定が入ります。
Kodi が最初に起動するのは二回目の起動なので、Kodi は最初から指定した言語で立ち上がる見込みです。

[^init]: <https://github.com/LibreELEC/LibreELEC.tv/blob/master/packages/sysutils/busybox/scripts/init>

### `mount-storage.sh` の実装上の注意

- `init` の関数の中で `.` により読み込まれるので、`$disk`、`mount_part`、`progress` などの変数と関数をそのまま使えます。ただし `exit` すると `init` 自体が終わるので使えません。`return` も `mount_storage` から抜ける点に注意します。
- 実行環境は initramfs の busybox です。`cp -a`、`mkdir -p`、`touch` 程度に限ります。
- `/flash` は `ro` でマウントされています。コピー元として読むだけなので再マウントは不要です。
- `mount_storage` は、`disk=` が指定されていない場合と `LIVE=yes` の場合にはこのフックを読み込みません。これらの構成では永続化されませんが、それで問題ありません。
- `OVERLAY` 構成では `$disk` にサブディレクトリが付与されてから読み込まれるので、通常のマウント呼び出しをそのまま書けば既定の動作と一致します。
- 利用者がすでに独自の `mount-storage.sh` を置いている場合の扱いは未決です（後述）。

## 宣言ファイル

`repack` は `--config`（`-c`）で宣言ファイルを受け取ります。
形式は YAML とし、スキーマを JSON Schema で定義して `jv`（aqua で導入済み）で検証します。

最初のバージョンで扱う項目は Kodi の言語だけにします。
キー名は草案です。

```yaml
kodi:
  language: resource.language.ja_jp
```

宣言ファイルを表す Go パッケージは、`internal/config` とは別にします（`internal/recipe` など、名前は未決）。
`internal/config` はアプリケーション自体の設定を持つパッケージであり、同じ名前の概念を混ぜると役割が曖昧になるからです。

## コマンドの形

```console
$ libreelec-repacker repack -c config.yaml -o out.img.gz LibreELEC.img.gz
```

- 入力は `inspect` と同じく raw または gzip です。
- 出力形式は `-o` の拡張子（`.img` か `.img.gz`）で決めます。専用フラグを設けるかは未決です。
- 出力後に `libreelec.Inspect` を出力イメージへ実行し、壊れていないことを確認します。

## 処理の流れ

1. 宣言ファイルを読み、スキーマで検証します。
2. 入力イメージを `inspect` と同じ方法で開き、boot パーティションを特定します。
3. 入力を raw の作業ファイルへ複製します（gzip なら展開します）。入力は最後まで読み取り専用で扱います。
4. 作業ファイルを書き込み可能で開き、boot パーティションの FAT に次を書き込みます。
   - `mount-storage.sh`
   - 種ファイルのディレクトリ（例：`repacker/storage/.kodi/userdata/guisettings.xml`。ディレクトリ名は未決）
5. 作業ファイルを出力先へ書き出します（必要なら gzip 圧縮します）。
6. 出力を検証します。

### `internal/diskimage` の読み取り専用の制約

`internal/diskimage` のパッケージドキュメントは「strictly read-only」を制約として明記しています。
この制約は、入力イメージを壊さないことを保証するためのものです。
そこで、入力の読み取りはこれまでどおり読み取り専用で行い、書き込みは複製した作業ファイルに限ります。
書き込み用の API を `diskimage` に足すか、別パッケージに分けるかは実装時に決めますが、どちらの場合もパッケージドキュメントの制約文を「入力に対しては読み取り専用」と実態に合わせて書き換えます。

go-diskfs v1.9.4 の FAT12/16/32 実装は `OpenFile`、`Mkdir`、`Remove` を持ち、書き込みに対応しています（FAT16 は FAT12 の上に実装されています）。
実イメージでの書き込みの動作は、まだ検証していません。

## 未確認の事項

この設計を作った環境からは LibreELEC の配布サーバーに接続できなかったので、実イメージと実機での検証をしていません。
次の点は実機での確認が必要です。

- **言語アドオンの有無**：LibreELEC に同梱される言語アドオンは英語だけの可能性が高いと考えています。`locale.language` に日本語を指定しても、`resource.language.ja_jp` がなければ Kodi は英語に戻る可能性があります。その場合はアドオン自体も種ファイルとして `/storage/.kodi/addons/` に配置する必要があり、手動で置いたアドオンを Kodi が無効として扱わないかも確認が必要です。
- **部分的な `guisettings.xml`**：`locale.language` だけを書いたファイルを Kodi が受け入れ、残りを既定値で補うかどうか。
- **LibreELEC の初回セットアップウィザード**：ウィザードが言語設定を上書きしないかどうか。
- **二回目の起動での Kodi の初回起動**：Kodi が本当に二回目の起動で初めて起動し、種ファイルのコピーより先に `guisettings.xml` を作らないかどうか。コピーは initramfs の段階で行うので先に終わるはずですが、実機で確認します。

## 未決の設計判断

- 宣言ファイルのキー名と、Go パッケージ名。
- 種ファイルを置く FAT 上のディレクトリ名と、適用済みマーカーの名前と場所。
- 利用者が独自の `mount-storage.sh` を置いたイメージを入力にした場合の扱い（エラーにするか、上書きするか、連結するか）。
- 出力形式の指定方法（拡張子だけか、フラグも設けるか）。
- boot パーティションの空き容量が足りない場合のエラーの出し方。

## 実装の順序

1. 宣言ファイルの最小スキーマと、それを読み込む Go パッケージ。
2. 入力を複製し、複製した側の FAT へ書き込む層。
3. `repack` コマンド（`mount-storage.sh` と `guisettings.xml` の配置、出力、検証）。
4. 実機での確認と、必要に応じた言語アドオンの配置。

各段階では、`CLAUDE.md` の規約（テーブル駆動テスト、`fmt.Errorf` による文脈付きのエラー、`slog`、パッケージドキュメントへの役割と制約の記述）に従います。
テスト用のイメージは `internal/diskimage/diskimagetest` の生成器を拡張して作ります。
