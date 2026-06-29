# `aws` command

`tronador aws` groups AWS resource automation commands for tagging,
secrets copying, default VPC cleanup, and security remediation.

```bash
tronador aws [global AWS flags] <command>
```

## Shared AWS flags

All `aws` subcommands accept the same authentication and region flags.

| Flag | Description |
| --- | --- |
| `--profile <name>` | AWS profile to use. |
| `--region <region>` | AWS region to use where the subcommand is region-scoped. |
| `--assume-role-arn <arn>` | ARN of the source role to assume. |
| `--assume-role-session-name <name>` | Session name for assume-role calls. |
| `--assume-role-external-id <id>` | External ID for assume-role calls. |
| `--assume-role-duration-secs <seconds>` | Assume-role session duration. Defaults to `3600`. |
| `--dry-run` | Show planned changes without making supported mutations. |
| `--verbose` | Print extra diagnostic output. |

## `aws tag`

Tags AWS resources with CloudOps Works organization metadata. The command uses
Resource Groups Tagging API plus native service discovery fallback for broader
coverage.

Supported resource families include EC2 resources, S3 buckets, IAM roles and
policies, SNS, SQS, Secrets Manager, ACM, KMS, AWS Backup, EventBridge event
buses, and schedule groups.

```bash
tronador aws tag \
  --organization "CloudOps" \
  --organization-unit "Platform" \
  --application-name "billing" \
  --application-type "service" \
  --target all \
  --types all
```

Important flags:

| Flag | Description |
| --- | --- |
| `--organization <value>` | Organization tag value. Required. |
| `--organization-unit <value>` | Organization unit tag value. Required. |
| `--application-name <value>` | Application name tag value. Required. |
| `--application-type <value>` | Application type tag value. Required. |
| `--managed-by <value>` | Managed-by tag value. Defaults to `manual`. |
| `--fullname-sep <value>` | Separator for generated organization full names. Defaults to `-`. |
| `--target resources|iam|all` | Resource scope. Defaults to `all`. |
| `--types <list>|all` | Comma-separated resource type list, or all supported types. |
| `--reapply` | Reapply tags even when resources already have tags. |
| `--include-service-linked` | Include service-linked IAM roles. |

## `aws copysecret`

Copies an AWS Secrets Manager secret within an account, across regions, or across
accounts using a destination assume role.

```bash
# Copy within the same account and region; destination defaults to source name.
tronador aws copysecret --source app/config

# Copy across regions.
tronador aws copysecret \
  --source app/config \
  --dest app/config-copy \
  --dest-region us-west-2

# Copy into another account.
tronador aws copysecret \
  --source app/config \
  --dest app/config \
  --dest-assume-role-arn arn:aws:iam::123456789012:role/SecretCopyRole
```

Flags:

| Flag | Description |
| --- | --- |
| `--source <name-or-arn>` | Source secret name or ARN. Required. |
| `--dest <name>` | Destination secret name. Defaults to the source name. |
| `--dest-region <region>` | Destination region for cross-region copies. |
| `--dest-assume-role-arn <arn>` | Destination account role ARN for cross-account copies. |

If the destination secret already exists, the command creates a new version
instead of replacing the secret resource.

## `aws remove-default-vpc`

Removes default VPCs across all AWS regions in the account. The command deletes
associated resources in dependency order, including internet gateways, subnets,
non-default security groups, and then the default VPC itself.

```bash
tronador aws remove-default-vpc --dry-run
tronador aws remove-default-vpc --exclude-regions us-west-2,eu-west-1
```

Flags:

| Flag | Description |
| --- | --- |
| `--exclude-regions <list>` | Comma-separated regions to skip. |

The parent `--region` flag does not limit this command; it intentionally checks
all regions unless excluded.

## `aws remediation`

Groups AWS security remediation helpers.

### `aws remediation s3`

Implements AWS Security Hub control S3-5 by ensuring S3 buckets deny requests
where `aws:SecureTransport` is false.

```bash
tronador aws remediation s3 --dry-run
tronador aws remediation s3
```

### `aws remediation ec2`

Implements AWS Security Hub control EC2-2 by removing unrestricted inbound and
outbound rules from default security groups in the current region.

```bash
tronador aws remediation ec2 --region us-east-1 --dry-run
tronador aws remediation ec2 --region us-east-1
```
