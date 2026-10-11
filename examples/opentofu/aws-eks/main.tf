# An EKS cluster with a PostgreSQL database and a bucket for the application. The application's service
# account gets access to the bucket through EKS Pod Identity.

locals {
  tags = {
    "distr.sh/instance"   = var.instance_name
    "distr.sh/managed-by" = "distr-opentofu-controller"
  }
  azs = slice(data.aws_availability_zones.available.names, 0, 3)
}

data "aws_availability_zones" "available" {
  state = "available"
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "6.7.3"

  name = var.instance_name
  cidr = "10.0.0.0/16"
  azs  = local.azs

  public_subnets   = [for i, _ in local.azs : cidrsubnet("10.0.0.0/16", 8, i)]
  private_subnets  = [for i, _ in local.azs : cidrsubnet("10.0.0.0/16", 4, i + 1)]
  database_subnets = [for i, _ in local.azs : cidrsubnet("10.0.0.0/16", 8, i + 100)]

  enable_nat_gateway                 = true
  single_nat_gateway                 = true
  create_database_subnet_group       = true
  create_database_subnet_route_table = true

  public_subnet_tags  = { "kubernetes.io/role/elb" = "1" }
  private_subnet_tags = { "kubernetes.io/role/internal-elb" = "1" }
}

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "21.29.0"

  name               = var.instance_name
  kubernetes_version = var.kubernetes_version

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  endpoint_public_access                   = true
  endpoint_public_access_cidrs             = var.endpoint_public_access_cidrs
  enable_cluster_creator_admin_permissions = true

  addons = {
    coredns                = {}
    eks-pod-identity-agent = { before_compute = true }
    kube-proxy             = {}
    vpc-cni                = { before_compute = true }
  }

  eks_managed_node_groups = {
    default = {
      instance_types = var.node_instance_types
      min_size       = var.node_count
      max_size       = var.node_count
      desired_size   = var.node_count
    }
  }
}

resource "random_password" "database" {
  length  = 32
  special = false
}

resource "aws_security_group" "database" {
  name        = "${var.instance_name}-database"
  description = "PostgreSQL from the EKS nodes"
  vpc_id      = module.vpc.vpc_id
}

resource "aws_vpc_security_group_ingress_rule" "database" {
  security_group_id            = aws_security_group.database.id
  referenced_security_group_id = module.eks.node_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}

resource "aws_db_instance" "database" {
  identifier     = var.instance_name
  engine         = "postgres"
  engine_version = "17"
  instance_class = var.database_instance_class

  allocated_storage     = 20
  max_allocated_storage = 100
  storage_encrypted     = true

  db_name  = "app"
  username = "app"
  password = random_password.database.result

  db_subnet_group_name   = module.vpc.database_subnet_group_name
  vpc_security_group_ids = [aws_security_group.database.id]

  backup_retention_period   = 7
  deletion_protection       = var.database_deletion_protection
  final_snapshot_identifier = "${var.instance_name}-final"
  apply_immediately         = true
}

resource "random_id" "bucket" {
  byte_length = 4
}

resource "aws_s3_bucket" "data" {
  bucket = "${var.instance_name}-data-${random_id.bucket.hex}"
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket                  = aws_s3_bucket.data.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "data" {
  bucket = aws_s3_bucket.data.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

data "aws_iam_policy_document" "pod_identity_trust" {
  statement {
    actions = ["sts:AssumeRole", "sts:TagSession"]
    principals {
      type        = "Service"
      identifiers = ["pods.eks.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "bucket_access" {
  statement {
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.data.arn]
  }
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.data.arn}/*"]
  }
}

resource "aws_iam_role" "application" {
  name               = "${var.instance_name}-application"
  assume_role_policy = data.aws_iam_policy_document.pod_identity_trust.json
}

resource "aws_iam_role_policy" "application" {
  name   = "bucket-access"
  role   = aws_iam_role.application.id
  policy = data.aws_iam_policy_document.bucket_access.json
}

resource "aws_eks_pod_identity_association" "application" {
  cluster_name    = module.eks.cluster_name
  namespace       = var.application_namespace
  service_account = var.application_service_account
  role_arn        = aws_iam_role.application.arn
}
