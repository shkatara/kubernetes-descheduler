package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// PodWithAffinity captures essential pod metadata for affinity-based scheduling.
type PodWithAffinity struct {
	HostIP            string
	CreationTimestamp time.Time
	Namespace         string
}

// main is the entry point for the descheduler. It orchestrates the workflow.
func main() {
	start := time.Now()
	logger := log.New(os.Stdout, "[descheduler] ", log.LstdFlags|log.Lmsgprefix)

	clientset, err := newKubernetesClient()
	if err != nil {
		logger.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	pods, err := listAllPods(clientset)
	if err != nil {
		logger.Fatalf("Failed to list pods: %v", err)
	}

	podsWithAffinity := selectPodsWithSpotAffinity(pods)
	if len(podsWithAffinity) == 0 {
		logger.Println("No pods with spot affinity found. Exiting.")
		return
	}

	nodes, err := listAllNodes(clientset)
	if err != nil {
		logger.Fatalf("Failed to list nodes: %v", err)
	}

	spotIPs := extractSpotNodeIPs(nodes)
	if len(spotIPs) == 0 {
		logger.Println("No spot nodes detected. Exiting.")
		return
	}

	podsDeleted, err := evictPodsNotOnSpot(clientset, podsWithAffinity, spotIPs)
	if err != nil {
		logger.Printf("Error deleting pods: %v", err)
	}

	for _, podName := range podsDeleted {
		logger.Printf("Pod %q evicted to reschedule on a spot node.", podName)
	}

	logger.Printf("Execution time: %v", time.Since(start))
}

// newKubernetesClient returns a new in-cluster Kubernetes clientset.
func newKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to get in-cluster config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create clientset: %w", err)
	}
	return clientset, nil
}

// listAllPods fetches all pods across all namespaces.
func listAllPods(clientset *kubernetes.Clientset) (*v1.PodList, error) {
	return clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
}

// listAllNodes fetches all nodes in the cluster.
func listAllNodes(clientset *kubernetes.Clientset) (*v1.NodeList, error) {
	return clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
}

// selectPodsWithSpotAffinity returns pods that have node affinity for spot nodes and are ready.
func selectPodsWithSpotAffinity(pods *v1.PodList) map[string]PodWithAffinity {
	result := make(map[string]PodWithAffinity)
	for _, pod := range pods.Items {
		if !hasSpotNodeAffinity(&pod) || !isPodReady(&pod) {
			continue
		}
		result[pod.GetName()] = PodWithAffinity{
			HostIP:            pod.Status.HostIP,
			CreationTimestamp: pod.GetCreationTimestamp().Time,
			Namespace:         pod.GetNamespace(),
		}
	}
	return result
}

// hasSpotNodeAffinity checks if a pod has node affinity for spot nodes.
func hasSpotNodeAffinity(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil {
		return false
	}
	for _, term := range affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		for _, expr := range term.Preference.MatchExpressions {
			if expr.Key == "cloud.google.com/gke-spot" && slices.Contains(expr.Values, "true") {
				return true
			}
		}
	}
	return false
}

// isPodReady returns true if the pod is in Ready condition.
func isPodReady(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

// extractSpotNodeIPs returns the IP addresses of all spot nodes.
func extractSpotNodeIPs(nodes *v1.NodeList) []string {
	var spotIPs []string
	for _, node := range nodes.Items {
		if node.Labels["cloud.google.com/gke-spot"] == "true" {
			for _, addr := range node.Status.Addresses {
				if addr.Type == v1.NodeInternalIP {
					spotIPs = append(spotIPs, addr.Address)
					break
				}
			}
		}
	}
	return spotIPs
}

// evictPodsNotOnSpot deletes pods with spot affinity that are not currently on spot nodes and are older than 10 minutes.
// evictPodsNotOnSpot deletes pods with spot affinity that are not currently on spot nodes and are older than 10 minutes.
// It returns a slice of pod names that were deleted, and an error if any deletion failed.
//
// The function works as follows:
//  1. It sets a cutoff time to 10 minutes before the current time. Only pods created before this cutoff are considered for eviction.
//  2. It iterates over the provided podsWithAffinity map, which contains pods that have spot node affinity.
//  3. For each pod, it checks:
//     a. If the pod is too new (created after the cutoff), it skips eviction for that pod.
//     b. If the pod is already running on a spot node (its HostIP is in the spotIPs list), it skips eviction.
//  4. For pods that are eligible for eviction, it launches a goroutine to delete the pod using the Kubernetes API.
//     a. It uses a WaitGroup to wait for all deletions to complete.
//     b. It uses a mutex to safely append deleted pod names to the podsDeleted slice from multiple goroutines.
//     c. If any deletion fails, it records the first error encountered using sync.Once to ensure only the first error is captured.
//  5. After all goroutines finish, it returns the list of deleted pod names and the first error encountered (if any).
func evictPodsNotOnSpot(clientset *kubernetes.Clientset, podsWithAffinity map[string]PodWithAffinity, spotIPs []string) ([]string, error) {
	var (
		mu          sync.Mutex     // Protects access to podsDeleted slice
		podsDeleted []string       // Names of pods successfully deleted
		wg          sync.WaitGroup // Waits for all goroutines to finish
		errOnce     sync.Once      // Ensures only the first error is recorded
		firstErr    error          // Stores the first error encountered
	)
	cutoff := time.Now().Add(-10 * time.Minute) // Only consider pods older than 10 minutes

	for podName, podInfo := range podsWithAffinity {
		// The following lines create new local variables within the loop scope,
		// capturing the current values of podName and podInfo for use inside the goroutine.
		// This prevents issues where the goroutine might reference the loop variables after they've changed.
		podName := podName
		podInfo := podInfo

		// Skip pods that are too new
		if podInfo.CreationTimestamp.After(cutoff) {
			continue
		}
		// Skip pods already on spot nodes
		if slices.Contains(spotIPs, podInfo.HostIP) {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Attempt to delete the pod
			err := clientset.CoreV1().Pods(podInfo.Namespace).Delete(context.Background(), podName, metav1.DeleteOptions{})
			if err != nil {
				// Record the first error encountered
				errOnce.Do(func() {
					firstErr = fmt.Errorf("failed to delete pod %q: %w", podName, err)
				})
				return
			}
			// Record the deleted pod name
			mu.Lock()
			podsDeleted = append(podsDeleted, podName)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return podsDeleted, firstErr
	}
	return podsDeleted, nil
}
