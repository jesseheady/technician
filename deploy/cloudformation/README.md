# CloudFormation: Technician on ECS Fargate

`ecs-fargate.yaml` stands up one Technician worker as an ECS Fargate service, with
Cloud Map DNS service discovery and an optional metrics path to Amazon Managed
Service for Prometheus.

It creates an ECS cluster, log group, Cloud Map private DNS namespace and service,
security group, task and execution roles, a task definition, and the service.

## Prerequisites

1. **Config in S3.** Upload the same files you would mount in Compose:

   ```
   s3://your-bucket/technician/prod/technician.yml
   s3://your-bucket/technician/prod/checks.yml     # or a checks/ directory
   s3://your-bucket/technician/prod/budgets.yml    # optional
   ```

   `technician.yml` must declare an origin whose `id` matches the `OriginId`
   parameter. That value becomes the `region` label on every metric.

2. **Image access.** The default is `ghcr.io/jesseheady/technician:latest`. Tasks
   in private subnets need a NAT path to ghcr.io, or mirror the image to ECR and
   pass `ImageUri`.

3. **Networking.** Two or more subnets. Private subnets need a NAT gateway (or VPC
   endpoints for ECR, S3, and CloudWatch Logs) plus outbound reachability to
   whatever your checks target.

## Deploy

```bash
aws cloudformation deploy \
  --template-file deploy/cloudformation/ecs-fargate.yaml \
  --stack-name technician-prod \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
      ConfigS3Uri=s3://your-bucket/technician/prod \
      OriginId=us-east-1 \
      VpcId=vpc-0123456789abcdef0 \
      SubnetIds=subnet-aaa,subnet-bbb
```

Updating checks means uploading new YAML and forcing a new deployment:

```bash
aws s3 sync ./config s3://your-bucket/technician/prod
aws ecs update-service --cluster technician-prod-cluster \
  --service technician-prod-technician --force-new-deployment
```

## Getting metrics out

The worker always exposes `/metrics` on port 9590. How you collect it is the
`MetricsMode` parameter.

### `ScrapeEndpointOnly` (default)

Nothing is added to the task. Point an existing scraper at the Cloud Map name from
the `MetricsDnsName` output, and grant your scraper's security group ingress on
9590 to the `SecurityGroupId` output. The template opens no ingress by default.

To use the **agentless CloudWatch managed collector**, create it out of band. It
has no CloudFormation resource: `AWS::APS::Scraper` only accepts
`Source: EksConfiguration`, while the VPC-connected collector that reaches ECS
takes `source.vpcConfiguration` and is only creatable through the API or CLI.

```bash
cat > scrape-config.yaml <<'EOF'
global:
  scrape_interval: 60s
scrape_configs:
  - job_name: technician
    dns_sd_configs:
      - names: ['technician.technician-prod.local']
        type: A
        port: 9590
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance
      - target_label: compute_platform
        replacement: ecs
EOF

aws amp create-scraper \
  --alias technician \
  --source '{"vpcConfiguration":{"subnetIds":["subnet-aaa","subnet-bbb"],"securityGroupIds":["sg-scraper"]}}' \
  --scrape-configuration configurationBlob=$(base64 -i scrape-config.yaml) \
  --destination '{"cloudWatchConfiguration":{"datasetArn":"arn:aws:cloudwatch:us-east-1:123456789012:dataset/default"}}'
```

Then allow `sg-scraper` ingress on 9590 to the stack's security group.

### `AdotSidecarToAmp`

Adds an AWS Distro for OpenTelemetry sidecar that scrapes `localhost:9590` and
remote-writes to AMP, and attaches an `aps:RemoteWrite` policy scoped to the
workspace. Requires `AmpWorkspaceArn`; a template rule rejects the stack at
creation time if it is missing.

Use this when you want everything in one stack, or when the managed collector is
not available in your region.

## Sizing

Defaults are 1024 CPU / 2048 MB, sized for browser checks. The worker container
reserves 512 MB as a soft floor and has no hard container limit, so a Chromium
spike can use task headroom instead of being OOM-killed at a fixed line.

| Workload | TaskCpu | TaskMemory | EnablePlaywright |
|---|---|---|---|
| Network checks only | 512 | 1024 | false |
| Mixed, occasional browser checks | 1024 | 2048 | true |
| Browser-heavy (`max_browsers` above 2) | 2048 | 4096 | true |

`EnablePlaywright=true` sets `initProcessEnabled` so exited Chromium children are
reaped. Without it zombie processes accumulate and leak kernel memory. Set it
whenever browser checks are configured.

See [deployment sizing](../../docs/deployment-sizing.md) for the measurements
behind these numbers.

## Things to know

**Run one task per origin.** `DesiredCount` defaults to 1 and should stay there.
Additional tasks re-run every check from the same origin and publish duplicate
results under identical metric labels. To cover more locations, deploy another
stack with a different `OriginId`.

**Status history is ephemeral.** `/var/lib/technician` is a task-scoped volume, so
`status.json` does not survive task replacement. The status page rebuilds from an
empty ring buffer, and because checks run once at startup it repopulates within
seconds. Metrics are unaffected: Prometheus or AMP holds the real history. Attach
EFS if you need the status page's few hours of history to survive a deployment.

**Config errors fail fast.** The init container runs `aws s3 sync` and asserts
`technician.yml` exists. A bad prefix fails the task at startup rather than leaving
a worker running with no checks.

**Not yet exercised against a live account.** The template is `cfn-lint` clean, but
has not been deployed end to end. Treat the first rollout as a validation run.
