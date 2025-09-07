resource "aws_cognito_user_pool" "pool" {
  name                = "wsgw-e2etest"
  username_attributes = ["email"]
  email_configuration {
    email_sending_account = "COGNITO_DEFAULT"
  }
}

resource "aws_cognito_user_pool_domain" "main" {
  domain       = "wsgw-e2etest-mvudg7noyqu"
  user_pool_id = aws_cognito_user_pool.pool.id
}

resource "aws_cognito_user_pool_ui_customization" "main" {
  user_pool_id = aws_cognito_user_pool_domain.main.user_pool_id
  css          = ".label-customizable {font-weight: 400;}"
}

resource "aws_cognito_user_group" "users" {
  name         = "USERS"
  user_pool_id = aws_cognito_user_pool.pool.id
}

resource "aws_cognito_user_group" "privileged_users" {
  name         = "PRIVILEGED_USERS"
  user_pool_id = aws_cognito_user_pool.pool.id
}

resource "aws_cognito_user" "testuser" {
  user_pool_id = aws_cognito_user_pool.pool.id
  username     = "no-reply@bitkit.click"
  password     = var.testuser_password

  attributes = {
    email          = "no-reply@bitkit.click"
    email_verified = true
  }
}

resource "aws_cognito_user_in_group" "testuser_users" {
  user_pool_id = aws_cognito_user_pool.pool.id
  group_name   = aws_cognito_user_group.users.name
  username     = aws_cognito_user.testuser.username
}

resource "aws_cognito_user" "privileged_testuser" {
  user_pool_id = aws_cognito_user_pool.pool.id
  username     = "no-reply-privileged@bitkit.click"
  password     = var.privileged_testuser_password

  attributes = {
    email          = "no-reply-privileged@bitkit.click"
    email_verified = true
  }
}

resource "aws_cognito_user_in_group" "privileged_testuser_users" {
  user_pool_id = aws_cognito_user_pool.pool.id
  group_name   = aws_cognito_user_group.users.name
  username     = aws_cognito_user.privileged_testuser.username
}

resource "aws_cognito_user_in_group" "privileged_testuser_privileged_users" {
  user_pool_id = aws_cognito_user_pool.pool.id
  group_name   = aws_cognito_user_group.privileged_users.name
  username     = aws_cognito_user.privileged_testuser.username
}
