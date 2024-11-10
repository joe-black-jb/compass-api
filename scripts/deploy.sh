# script.sh
#!/bin/bash

# 中断シグナル（SIGINT）をキャッチしてクリーンアップ処理を実行
cleanup() {
    echo "\033[31mスクリプトが中断されました。後続の処理は実行されません。\033[0m"  # 赤色
    exit 1
}

# SIGINT (Ctrl+C) をキャッチするように trap を設定
trap cleanup SIGINT

# .env ファイルを読み込む
if [ -f .env ]; then
    echo "\033[32m.env ファイルが見つかりました\033[0m"  # 緑色
    source .env
else
    echo "\033[31m.env ファイルが見つかりません\033[0m"  # 赤色
    exit 1
fi

# 引数を取得
TARGET=$1
echo "\033[33mTARGET: $1\033[0m"  # 黄色

# Build Docker image
# docker build --platform linux/amd64 -t $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG .
docker build --platform linux/amd64 -t compass-api:${IMAGE_TAG} .

### ECR に push するためにタグを付け替える
docker tag compass-api:${IMAGE_TAG} ${AWS_ACCOUNT_ID}.dkr.ecr.ap-northeast-1.amazonaws.com/compass:${IMAGE_TAG}

# Login to ECR
aws ecr get-login-password --region ap-northeast-1 | docker login --username AWS --password-stdin $ECR_BASE_URI

# Push Docker image to ECR
docker push $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG

# Update Lambda function code
# 引数に応じて Lambda 関数を更新
if [ "$TARGET" == "private" ]; then
    # private の場合は LAMBDA_FUNCTION_NAME のみ更新
    echo "\033[33mUpdating private Lambda function: $LAMBDA_FUNCTION_NAME\033[0m"  # 黄色
    aws lambda update-function-code --function-name $LAMBDA_FUNCTION_NAME --image-uri $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG | jq
elif [ "$TARGET" == "public" ]; then
    # public の場合は LAMBDA_FUNCTION_NAME_PUBLIC のみ更新
    echo "\033[33mUpdating public Lambda function: $LAMBDA_FUNCTION_NAME_PUBLIC\033[0m"  # 黄色
    aws lambda update-function-code --function-name $LAMBDA_FUNCTION_NAME_PUBLIC --image-uri $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG | jq
else
    # 引数がない場合は両方の Lambda 関数を更新
    echo "\033[36mUpdating both private and public Lambda functions\033[0m"  # シアン
    aws lambda update-function-code --function-name $LAMBDA_FUNCTION_NAME --image-uri $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG | jq
    aws lambda update-function-code --function-name $LAMBDA_FUNCTION_NAME_PUBLIC --image-uri $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG | jq
fi

# echo "\033[33mUpdating private Lambda function: $LAMBDA_FUNCTION_NAME\033[0m"  # 黄色
#     aws lambda update-function-code --function-name $LAMBDA_FUNCTION_NAME --image-uri $ECR_BASE_URI/$ECR_NAME:$IMAGE_TAG | jq