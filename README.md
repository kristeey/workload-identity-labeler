# Workload Identity Labeler Controller

This project is a Kubernetes controller written in Go. It periodically scans all ServiceAccounts in the cluster. If a ServiceAccount has the label `workload.identity.labeler/azure-mi-client-name` and does not have the annotation `azure.workload.identity/client-id`, the controller fetches the corresponding Azure Managed Identity (MI) client ID and adds the `azure.workload.identity/client-id` annotation. Followingly it will do a rolling restart of all deployments referencing this Service Account.

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

When installing with the helm chart, simply set the `azure.clientId` value to the client ID of the Azure Identity you want to use for authentication.


For more details, see the [Azure Workload Identity documentation](https://azure.github.io/azure-workload-identity/docs/).

### Azure client secret (not recommended for production)
You can let **Workload-identity-labeler Controller** authenticate against Azure using Client secrets belonging to the Azure Client you want to use.

When installing with the helm chart, populate the following values: `azure.authMode: "service-principal"`, `azure.clientId`, `azure.clientSecret`, `azure.tenantId`.


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
Download repo
```bash
helm repo add workload-identity-labeler https://raw.githubusercontent.com/kristeey/workload-identity-labeler/gh-pages
helm repo update
```

#### Installing in `workload-identity` authentication mode:
>**_Requires_**:
>1. An azure client, either a managed identity or a service principal.
>2. A federated credential for the `workload-indentity-labeler-sa` attached to the azure client.

Insert subscription ID (the subscription the WI-labeler will look for managed identities), and the Azure client ID (that the WI-labeler will authenticate as to be able find the managed identities in azure).

```bash
helm install \
  workload-identity-labeler workload-identity-labeler/workload-identity-labeler \
  --set azure.subscriptionId="<some-value>" \
  --set azure.clientId="<some-clientID>"
```
#### Alternatively, installing in `service-principal` authentication mode:
>**_Requires_**:
>1. An azure client, either a managed identity or a service principal.
>2. A client secret or cert attached to the azure client.

Insert subscription ID (the subscription the WI-labeler will look for managed identities), the Azure client ID (that the WI-labeler will authenticate as to be able find the managed identities in azure), the Azure Client secret and the Azure tenant ID

```bash
helm install \
  workload-identity-labeler workload-identity-labeler/workload-identity-labeler \
  --set azure.authMode="service-principal" \
  --set azure.subscriptionId="<some-subscriptionID>" \
  --set azure.clientId="<some-clientID>" \
  --set azure.clientSecret="<some-clientSecret>" \
  --set azure.tenantID="<some-tenantID>
```

### Insalling using k8s manifests (example)
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
