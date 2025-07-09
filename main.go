package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var logger *slog.Logger

func init() {
	level := strings.ToLower(os.Getenv("LOG_LEVEL"))
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "error":
		slogLevel = slog.LevelError
	case "warn":
		slogLevel = slog.LevelWarn
	default:
		slogLevel = slog.LevelInfo
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}

func main() {
	// Setup clients
	clientset, msiClient, interval, err := setup()
	if err != nil {
		logger.Error("failed to setup clients", "err", err)
		return
	}

	for {
		// List all ServiceAccounts in the cluster
		logger.Debug("Listing all ServiceAccounts in the cluster")
		sas, err := clientset.CoreV1().ServiceAccounts("").List(context.Background(), metav1.ListOptions{})
		if err == nil {
			for _, sa := range sas.Items {
				logger.Debug("Found ServiceAccount", "namespace", sa.Namespace, "name", sa.Name)
			}
		} else {
			logger.Error("failed to list service accounts", "err", err)
			return
		}
		// Check for WI label and Label ServiceAccounts with the Azure Managed Identity client ID
		changedSAs, err := labelServiceAccounts(sas, msiClient, clientset)
		if err != nil {
			logger.Error("error running labelServiceAccounts", "err", err)
		}
		// search for deployments using the changed ServiceAccounts
		if len(changedSAs) > 0 {
			logger.Info("Found ServiceAccounts with workload.identity.labeler label, checking for deployments using them", "changedServiceAccounts", changedSAs)
			deployments, err := searchDeploymentsForSAs(clientset, changedSAs)
			if err != nil {
				logger.Error("failed to search deployments for ServiceAccounts", "err", err)
			}
			if len(deployments) > 0 {
				logger.Info("Found deployments using ServiceAccounts with workload.identity.labeler label", "deployments", len(deployments))
				for _, dep := range deployments {
					logger.Info("Deployment using ServiceAccount with workload.identity.labeler label", "namespace", dep.Namespace, "name", dep.Name, "serviceAccountName", dep.Spec.Template.Spec.ServiceAccountName)
					// Rollout restart the deployment
					err := rolloutRestartDeployment(clientset, dep.Namespace, dep.Name)
					if err != nil {
						logger.Error("failed to rollout restart deployment", "namespace", dep.Namespace, "name", dep.Name, "err", err)
					} else {
						logger.Info("Rolled out restart for deployment", "namespace", dep.Namespace, "name", dep.Name)
					}
				}
			} else {
				logger.Info("No deployments found using ServiceAccounts with workload.identity.labeler label")
			}

		}
		time.Sleep(interval)
	}
}

func setup() (*kubernetes.Clientset, *armmsi.UserAssignedIdentitiesClient, time.Duration, error) {
	interval := getScanInterval()
	logger.Info("Starting workload-identity-labeler controller", "scan_interval", interval.String())

	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("failed to get in-cluster config", "err", err)
		return nil, nil, interval, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error("failed to create k8s client", "err", err)
		return nil, nil, interval, err
	}
	subID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		logger.Error("failed to get Azure credential", "err", err)
		return nil, nil, interval, err
	}
	msiClient, err := armmsi.NewUserAssignedIdentitiesClient(subID, cred, nil)
	if err != nil {
		logger.Error("failed to create Azure MSI client", "err", err)
		return nil, nil, interval, err
	}

	return clientset, msiClient, interval, nil
}

func getScanInterval() time.Duration {
	val := os.Getenv("INTERVAL")
	if val == "" {
		return 60 * time.Second
	}
	dur, err := time.ParseDuration(val)
	if err != nil {
		logger.Warn("Invalid INTERVAL, using default 60s", "err", err)
		return 60 * time.Second
	}
	return dur
}

func labelServiceAccounts(sas *v1.ServiceAccountList, msiClient *armmsi.UserAssignedIdentitiesClient, clientset *kubernetes.Clientset) ([]string, error) {
	logger.Info("Scanning ServiceAccounts for Azure workload.identity.labeler label...")
	var changedSAs []string
	for _, sa := range sas.Items {
		labels := sa.Labels
		if labels == nil {
			logger.Debug("ServiceAccount has no labels", "namespace", sa.Namespace, "name", sa.Name)
			continue
		}
		annotations := sa.Annotations
		if annotations == nil {
			annotations = map[string]string{}
		}
		// Check if the ServiceAccount has the workload.identity.labeler label
		miName, hasMILabel := labels["workload.identity.labeler/azure-mi-client-name"]
		_, hasClientIDAnnotation := annotations["azure.workload.identity/client-id"]
		if hasMILabel && !hasClientIDAnnotation && miName != "" {
			logger.Info("Found ServiceAccount with workload.identity.labeler label", "namespace", sa.Namespace, "name", sa.Name, "miName", miName)
			clientID, err := findAzureClientID(msiClient, miName)
			if err != nil {
				logger.Warn("failed to get client id", "miName", miName, "err", err)
				continue
			}
			annotations["azure.workload.identity/client-id"] = clientID
			sa.Annotations = annotations
			_, err = clientset.CoreV1().ServiceAccounts(sa.Namespace).Update(context.Background(), &sa, metav1.UpdateOptions{})
			if err != nil {
				logger.Warn("failed to update ServiceAccount", "namespace", sa.Namespace, "name", sa.Name, "err", err)
			} else {
				logger.Info("Updated ServiceAccount with client-id annotation", "namespace", sa.Namespace, "name", sa.Name)
				changedSAs = append(changedSAs, sa.Name)
			}
		} else if hasClientIDAnnotation {
			logger.Debug("ServiceAccount already has azure.workload.identity/client-id annotation", "namespace", sa.Namespace, "name", sa.Name)
			continue
		}
	}
	return changedSAs, nil
}

func findAzureClientID(msiClient *armmsi.UserAssignedIdentitiesClient, miName string) (string, error) {
	pager := msiClient.NewListBySubscriptionPager(nil)
	for pager.More() {
		resp, err := pager.NextPage(context.Background())
		if err != nil {
			logger.Error("failed to page managed identities", "err", err)
			return "", err
		}
		for _, id := range resp.Value {
			if id.Name != nil && *id.Name == miName && id.Properties != nil && id.Properties.ClientID != nil {
				return *id.Properties.ClientID, nil
			}
		}
	}
	logger.Error("managed identity not found", "miName", miName)
	return "", os.ErrNotExist
}

func searchDeploymentsForSAs(clientset *kubernetes.Clientset, saRefs []string) ([]appsv1.Deployment, error) {
	var found []appsv1.Deployment
	deployments, err := clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments.Items {
		saName := dep.Spec.Template.Spec.ServiceAccountName
		for _, ref := range saRefs {
			if saName == ref {
				found = append(found, dep)
				break
			}
		}
	}
	return found, nil
}

func rolloutRestartDeployment(clientset *kubernetes.Clientset, namespace, deploymentName string) error {
	depClient := clientset.AppsV1().Deployments(namespace)
	dep, err := depClient.Get(context.Background(), deploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)
	_, err = depClient.Update(context.Background(), dep, metav1.UpdateOptions{})
	return err
}
