# Helm chart for workload-identity-labeler

This chart deploys the workload-identity-labeler controller, which annotate ServiceAccounts with Azure client IDs.

## Configuration

See `values.yaml` for all configurable options, including:
- Log level
- Scan interval
- Image repository and tag

## Usage

```bash
helm install workload-identity-labeler ./deploy/helm/workload-identity-labeler \
  --set azure.subscriptionId=... \
  --set image.repository=<your-repo>/workload-identity-labeler \
  --set image.tag=<your-tag>
```

You can also override any value in `values.yaml` using `--set` or a custom values file.
