# compass-api

企業分析アプリのバックエンド

## コマンド

### マイグレーション (旧)

```sh
make migrate
```

### 財務諸表データ登録バッチ

```sh
make xbrl
```

### デプロイ (コマンド)

```sh
# Docker イメージをビルド
docker build --platform linux/amd64 -t compass-api:${IMAGE_TAG} .

# ビルド (キャッシュなし)
docker build --no-cache --platform linux/amd64 -t compass-api:${IMAGE_TAG} .

# ECR に push するためにタグを付け替える
docker tag compass-api:${IMAGE_TAG} ${AWS_ACCOUNT_ID}.dkr.ecr.ap-northeast-1.amazonaws.com/compass:${IMAGE_TAG}

# ECR にログイン
aws ecr get-login-password --region ap-northeast-1 | docker login --username AWS --password-stdin $ECR_BASE_URI

# Docker イメージを ECR に push
docker push $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG

## Lambda 関数を更新
aws lambda update-function-code --function-name compass-api --image-uri ${ECR_BASE_URI}/compass:${IMAGE_TAG}
```

### デプロイ (スクリプト)

```sh
# private / public 双方
make deploy

# private
make deploy TARGET=private

# public
make deploy TARGET=public
```

### ローカルで Docker イメージを作成

```sh
# Docker イメージの確認
docker images

# Docker コンテナの起動
docker run --rm -it {確認したイメージID}
```

### コマンド (LocalStack)

```sh
# Docker コンテナの作成
make up

# zipファイルの作成
make zip

# Lambda を更新
make local-lambda-update

# LocalStack + Terraform でローカルに AWS 環境を構築
make tf

# ローカルでAPIリクエスト (private / public は不要)
curl --location 'http://localhost:4566/restapis/{api_id}/dev/_user_request_/{path}'
```
