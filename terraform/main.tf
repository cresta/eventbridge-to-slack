# eventbridge-to-slack
#
# Send eventbridge messages to slack

variable "name" {
  type        = string
  description = "Name of this lambda.  Should be account unique."
  default     = "eventbridge-to-slack"
}

variable "repository_name" {
  type        = string
  description = "Name of ECR repository"
  default     = "eventbridge-to-slack"
}

variable "image_tag" {
  type        = string
  description = "tag to use for image in lambda"
  default     = "latest"
}

variable "filter_regex" {
  type    = string
  default = ""
}

variable "filter_template" {
  type    = string
  default = ""
}

variable "slack_client_secret" {
  type    = string
  default = ""
}

variable "slack_channel" {
  type    = string
  default = ""
}

variable "msg_to_send" {
  type    = string
  default = ""
}

resource "aws_iam_role" "this" {
  name               = "${var.name}-lambda-role"
  description        = "Basic lambda role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json
}

data "aws_iam_policy_document" "assume_role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# Used for cloudwatch logs
resource "aws_iam_role_policy_attachment" "lambdabasic" {
  role       = aws_iam_role.this.id
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_ecr_repository" "this" {
  name                 = var.repository_name
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }
}

# https://docs.aws.amazon.com/AmazonECR/latest/userguide/LifecyclePolicies.html#lifecycle_policy_syntax
resource "aws_ecr_lifecycle_policy" "this" {
  repository = aws_ecr_repository.this.name
  policy = jsonencode(
    {
      rules : [
        {
          rulePriority : 1
          description : "Keep lots of master tags",
          selection : {
            tagStatus : "tagged",
            tagPrefixList : ["master", "main"],
            countType : "imageCountMoreThan",
            countNumber : 40
          },
          action : {
            type : "expire"
          }
        },
        {
          rulePriority : 2
          description : "Keep lots of version tags",
          selection : {
            tagStatus : "tagged",
            tagPrefixList : ["v"],
            countType : "imageCountMoreThan",
            countNumber : 20
          },
          action : {
            type : "expire"
          }
        },
        {
          rulePriority : 3
          description : "Do not keep lots of untagged images",
          selection : {
            tagStatus : "untagged",
            countType : "imageCountMoreThan",
            countNumber : 20
          },
          action : {
            type : "expire"
          }
        },
        {
          rulePriority : 4
          description : "Limit how many we keep in total",
          selection : {
            tagStatus : "any",
            countType : "imageCountMoreThan",
            countNumber : 50
          },
          action : {
            type : "expire"
          }
        }
      ]
    }
  )
}


# From https://github.com/giuseppeborgese/terraform-aws-secret-manager-with-rotation/blob/master/main.tf
resource "aws_lambda_function" "this" {
  function_name = var.name
  role          = aws_iam_role.this.arn
  timeout       = 30
  description   = "Turn lambda events into slack messages"
  package_type  = "Image"
  image_uri     = "${aws_ecr_repository.this.repository_url}:${var.image_tag}"
  image_config {
    entry_point = ["/main"]
  }
  environment {
    variables = {
      FILTER_REGEX        = var.filter_regex
      FILTER_TEMPLATE     = var.filter_template
      MSG_TO_SEND         = var.msg_to_send
      SLACK_CHANNEL       = var.slack_channel
      SLACK_CLIENT_SECRET = var.slack_client_secret
    }
  }
}

output "lambda_arn" {
  value       = aws_lambda_function.this.arn
  description = "Lambda ARN"
}
