# compass-api
企業分析アプリのバックエンド

## 環境構築

1. 環境変数設定ファイルの配置
ルートディレクトリに .env ファイルを配置（内容は以下の通り）
```sh
MYSQL_DATABASE={DB名}
MYSQL_ROOT_PASSWORD={ルートユーザのパスワード}
```  
2. docker-compose で起動
