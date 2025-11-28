locals {
  dynamodb_url = "http://dynamodb-k8s:8000"
}

provider "aws" {
  access_key                  = "mockAccessKey"
  region                      = "eu-west-1"
  secret_key                  = "mockSecretKey"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    dynamodb = local.dynamodb_url
  }
}

resource "aws_dynamodb_table" "WsgwConnectionIds" {
  name           = "WsgwConnectionIds"
  billing_mode   = "PROVISIONED"
  read_capacity  = 5
  write_capacity = 5
  hash_key       = "UserId"
  range_key      = "ConnectionId"

  attribute {
    name = "UserId"
    type = "S"
  }

  attribute {
    name = "ConnectionId"
    type = "S"
  }
}
