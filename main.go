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
		logger.Error("Failed to setup clients", "err", err)
		return
	}

	for {
		// List all ServiceAccounts in the cluster
		logger.Debug("Listing all ServiceAccounts in the cluster")
		sas, err := clientset.CoreV1().ServiceAccounts("").List(context.Background(), metav1.ListOptions{})
		if err == nil {
			for _, sa := range sas.Items {
				logger.Debug("Found ServiceAccount", "name", sa.Name, "namespace", sa.Namespace)
			}
		} else {
			logger.Error("Failed to list service accounts", "err", err)
			return
		}
		// Check for WI label and Label ServiceAccounts with the Azure Managed Identity client ID
		changedSAs, err := labelServiceAccounts(sas, msiClient, clientset)
		if err != nil {
			logger.Error("Error running labelServiceAccounts", "err", err)
		}
		// Search for deployments using the changed ServiceAccounts
		// If any deployments are found, rollout restart them
		if len(changedSAs) > 0 {
			logger.Info("Changed ServiceAccounts", "ServiceAccounts", changedSAs)
			depObj, err := searchDeploymentsForSAs(clientset, changedSAs)
			if err != nil {
				logger.Error("Failed to search deployments for ServiceAccounts", "err", err)
			}
			if len(depObj) > 0 {
				restartedDeps, err := rolloutRestartDeployments(clientset, depObj)
				if err != nil {
					logger.Error("Failed to restart deployments", "err", err)
				}
				if len(restartedDeps) > 0 {
					logger.Info("Restarted Deployments", "deployments", restartedDeps)
				} else {
					logger.Info("No deployments were restarted")
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
		logger.Error("Failed to get in-cluster config", "err", err)
		return nil, nil, interval, err
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error("Failed to create k8s client", "err", err)
		return nil, nil, interval, err
	}
	subID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		logger.Error("Failed to get Azure credential", "err", err)
		return nil, nil, interval, err
	}
	msiClient, err := armmsi.NewUserAssignedIdentitiesClient(subID, cred, nil)
	if err != nil {
		logger.Error("Failed to create Azure MSI client", "err", err)
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
	logger.Info("Scanning for ServiceAccounts...")
	var changedSAs []string
	for _, sa := range sas.Items {
		labels := sa.Labels
		if labels == nil {
			logger.Debug("ServiceAccount has no labels", "name", sa.Name, "namespace", sa.Namespace)
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
			logger.Debug("Found ServiceAccount", "name", sa.Name, "namespace", sa.Namespace, "miName", miName)
			clientID, err := findAzureClientID(msiClient, miName)
			if err != nil {
				logger.Warn("Failed to get client id", "miName", miName, "err", err)
				continue
			}
			annotations["azure.workload.identity/client-id"] = clientID
			sa.Annotations = annotations
			_, err = clientset.CoreV1().ServiceAccounts(sa.Namespace).Update(context.Background(), &sa, metav1.UpdateOptions{})
			if err != nil {
				logger.Warn("Failed to update ServiceAccount", "name", sa.Name, "namespace", sa.Namespace, "err", err)
			} else {
				logger.Debug("Updated ServiceAccount with client-id annotation", "name", sa.Name, "namespace", sa.Namespace)
				changedSAs = append(changedSAs, sa.Name)
			}
		} else if hasClientIDAnnotation {
			logger.Debug("ServiceAccount already has azure.workload.identity/client-id annotation", "name", sa.Name, "namespace", sa.Namespace)
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
			logger.Error("Failed to page managed identities", "err", err)
			return "", err
		}
		for _, id := range resp.Value {
			if id.Name != nil && *id.Name == miName && id.Properties != nil && id.Properties.ClientID != nil {
				return *id.Properties.ClientID, nil
			}
		}
	}
	logger.Error("Managed identity not found. Check if the authenticated identity (azure client) has reader access on correct scope.", "miName", miName)
	return "", os.ErrNotExist
}

func searchDeploymentsForSAs(clientset *kubernetes.Clientset, saRefs []string) (foundDeps []appsv1.Deployment, err error) {
	deployments, err := clientset.AppsV1().Deployments("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments.Items {
		saName := dep.Spec.Template.Spec.ServiceAccountName
		for _, ref := range saRefs {
			if saName == ref {
				foundDeps = append(foundDeps, dep)
				break
			}
		}
	}
	return foundDeps, nil
}

func rolloutRestartDeployments(clientset *kubernetes.Clientset, deployments []appsv1.Deployment) (restartedDeps []string, err error) {
	for _, dep := range deployments {
		depClient := clientset.AppsV1().Deployments(dep.Namespace)
		// Get the latest version in case of resourceVersion conflicts
		freshDep, err := depClient.Get(context.Background(), dep.Name, metav1.GetOptions{})
		if err != nil {
			logger.Error("Failed to get deployment", "name", dep.Name, "namespace", dep.Namespace, "err", err)
			continue
		}
		if freshDep.Spec.Template.Annotations == nil {
			freshDep.Spec.Template.Annotations = map[string]string{}
		}
		freshDep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)
		_, err = depClient.Update(context.Background(), freshDep, metav1.UpdateOptions{})
		if err != nil {
			logger.Error("Failed to update deployment", "name", dep.Name, "namespace", dep.Namespace, "err", err)
			continue
		} else {
			logger.Debug("Restarted deployment", "name", dep.Name, "namespace", dep.Namespace)
			restartedDeps = append(restartedDeps, dep.Name)
		}
	}
	return restartedDeps, nil
}
