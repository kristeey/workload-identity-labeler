# Workload Identity Labeler Controller

This project is a Kubernetes controller written in Go. It periodically scans all ServiceAccounts in the cluster. If a ServiceAccount has the label `workload.identity.labeler/azure-mi-client-name` and does not have the annotation `azure.workload.identity/client-id`, the controller fetches the corresponding Azure Managed Identity client ID and adds the `azure.workload.identity/client-id` annotation. Followingly it will do a rolling restart of all deployments referencing this Service Account.

## How it works
- Scans all ServiceAccounts every scan interval.
- If a ServiceAccount has the MI label and is missing the client-id label, it fetches the client ID from Azure and updates the ServiceAccount with `azure.workload.identity/client-id` annotation
- Does nothing if the ServiceAccount is missing the `workload.identity.labeler/azure-mi-client-name` label or already has the `azure.workload.identity/client-id` annotation.
- For each ServiceAccount that changed, the controller will look for Deployments that reference it in the pod spec (`.spec.template.spec.serviceAccountName`), and perform a rollout restart of these in order to ensure the pods has the correct injected workload identity environment variables.

## Configuration
Set the following environment variables for Azure authentication:
REQUIRED
- `AZURE_SUBSCRIPTION_ID`: Azure subscription to scan for MIs.
OPTIONAL
- `AZURE_TENANT_ID`: Azure tenant id. Needed for Client secret authentication
- `AZURE_CLIENT_ID`: Azure client used to authenticate against Azure. Needed for Client secret authentication.
- `AZURE_CLIENT_SECRET`: Client secret Azure client specified with `AZURE_CLIENT_ID`. Not recomended for production, see section for workload identity.
- `LOG_LEVEL`: Log level, either of `debug`, `info`, `warn` or `error`. Defaults to `info`.
- `INTERVAL`: Scan interval. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h". Defaults to `60s`.

## Authentication

### Workload Identity (recommended for production)

This controller is designed to work seamlessly with Azure Workload Identity for Kubernetes. Workload Identity allows Kubernetes workloads to access Azure resources securely without managing secrets. When using Workload Identity, ensure your ServiceAccounts are annotated and labeled according to the [Azure Workload Identity documentation](https://azure.github.io/azure-workload-identity/docs/).

To utilize Azure workload identity as authentication method for the **Workload-identity-labeler Controller** do the following:
1. Add `azure.workload.identity/use: "true"` pod annotation.
2. Add `azure.workload.identity/client-id: "<client-id>"` kubernetes service account label.

### Azure client secret (not recommended for production)
You can let **Workload-identity-labeler Controller** authenticate against Azure using Client secrets belonging to the Azure Client you want to use. Simply specify the `AZURE_TENANT_ID`, `AZURE_CLIENT_ID` and `AZURE_CLIENT_SECRET` environment variables.


**Key points:**
- The controller expects your Kubernetes ServiceAccounts to have the label `workload.identity.labeler/azure-mi-client-name` with the value set to the Azure Managed Identity name.
- The controller will automatically add the `azure.workload.identity/client-id` label if it is missing.
- For production, prefer Workload Identity over client secrets for improved security and manageability.

For more details, see the [Azure Workload Identity documentation](https://azure.github.io/azure-workload-identity/docs/).

## Permissions
### Required Kubernetes Roles

The controller's ServiceAccount must have a ClusterRole with permissions to:
- List, get, watch, and update ServiceAccounts
- List, get, watch, update, and patch Deployments (in the "apps" API group)

If installing using Helm chart, this will be given by default.

---

### Required Azure Roles

The Azure identity (Service Principal or Managed Identity) used by the controller must have permissions to:
- List user-assigned managed identities in the subscription.

Recommended Azure role:
- **Reader** on the subscription or resource group containing the managed identities.

---

## Docker
```bash
docker build -t workload-identity-labeler:latest .
```
For cross-plattform build use `docker buildx`. For instance, build for amd64 linux architecture:
```bash
docker buildx build --platform linux/amd64 -t workload-identity-labeler:latest .
```
## Installation
### Helm
TBA

## Example
1. Edit `deploy/k8s/deployment.yaml` to set your image and Azure credentials.
2. Apply the **Workload-identity-labeler**:
  ```bash
  kubectl apply -f deploy/k8s/wi-labeler-deployment.yaml
  ```
3. Apply a test deployment for a app that is going to use workload identity.
  ```bash
  kubectl apply -f deploy/k8s/test-deployment.yaml
  ```
3. Edit service account annotation to contain a valid MI name (`deploy/k8s/test-sa.yaml`)
4. Apply Service account and verify that the WI label is added.
  ```bash
  kubectl apply -f deploy/k8s/test-sa.yaml
  ```
5. Check the logs of the **Workload-identity-labeler**
  ```bash
  kubectl logs svc/workload-identity-labeler -f
  ```
