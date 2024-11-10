# compass-api

企業分析アプリのバックエンド

## コマンド

```sh
# ローカルサーバ起動
air

# マイグレーション
make migrate

# 財務諸表登録
make xbrl

# デプロイ
## Docker
### ビルド
docker build --platform linux/amd64 -t compass-api:${IMAGE_TAG} .

### ビルド (キャッシュなし)
docker build --no-cache --platform linux/amd64 -t compass-api:${IMAGE_TAG} .

### ECR に push するためにタグを付け替える
docker tag compass-api:${IMAGE_TAG} 087756241789.dkr.ecr.ap-northeast-1.amazonaws.com/compass:${IMAGE_TAG}

## Login to ECR
aws ecr get-login-password --region ap-northeast-1 | docker login --username AWS --password-stdin $ECR_BASE_URI

## Push Docker image to ECR
docker push $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG

## Update Lambda Function
aws lambda update-function-code --function-name compass-api --image-uri ${ECR_BASE_URI}/compass:${IMAGE_TAG}


# Docker
## ローカルでコンテナを起動する
docker images

docker run --rm -it {確認したイメージID}
```

## コマンド (LocalStack)

```sh
# Docker コンテナの作成
make up

# zipファイルの作成
make zip

# Lambda を更新
make local-lambda-update

# LocalStack + Terraform でローカルに AWS 環境を構築
make tf
```
