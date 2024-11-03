######### Dynamo DB テーブルは一度作成したらコメントアウト #########
resource "aws_dynamodb_table" "compass-dynamodb-table" {
  name           = "compass_companies"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "id"

  attribute {
    name ="id"
    type ="S"
  }

  attribute {
    name = "name"
    type = "S"
  }

  attribute {
    name = "edinetCode"
    type = "S"
  }

  attribute {
    name = "bs"
    type = "N"
  }

  attribute {
    name = "pl"
    type = "N"
  }

  global_secondary_index {
    name               = "CompanyNameIndex"
    hash_key           = "name"
    range_key          = "edinetCode"
    write_capacity     = 10
    read_capacity      = 10
    projection_type    = "INCLUDE"
    non_key_attributes = ["id"]
  }


  tags = {
    Name        = "Name"
    Environment = "compass_companies"
  }
}
