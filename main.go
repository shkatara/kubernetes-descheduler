package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type PodWithAffinity struct {
	HostIP            string
	CreationTimestamp time.Time
	Namespace         string
}

type SpotInstance struct {
	HostIP string
}

func main() {
	start := time.Now()

	clientset, err := createKubernetesClient()
	if err != nil {
		panic(fmt.Errorf("failed to create Kubernetes client: %w", err))
	}

	podsWithAffinity, err := findPodsWithAffinity(clientset)
	if err != nil {
		panic(fmt.Errorf("failed to find pods with affinity: %w", err))
	}

	spotIPs, err := getSpotInstanceIPs(clientset)
	if err != nil {
		panic(fmt.Errorf("failed to get spot instance IPs: %w", err))
	}

	err = deleteNonSpotPods(clientset, podsWithAffinity, spotIPs)
	if err != nil {
		panic(fmt.Errorf("failed to delete non-spot pods: %w", err))
	}

	fmt.Printf("Execution time: %v\n", time.Since(start))
}

func createKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func findPodsWithAffinity(clientset *kubernetes.Clientset) (map[string]PodWithAffinity, error) {
	podsWithAffinity := make(map[string]PodWithAffinity)

	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
			continue
		}

		for _, term := range pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
			for _, expr := range term.Preference.MatchExpressions {
				if expr.Key == "cloud.google.com/gke-spot" && slices.Contains(expr.Values, "true") {
					if isPodReady(pod) {
						podsWithAffinity[pod.GetName()] = PodWithAffinity{
							HostIP:            pod.Status.HostIP,
							CreationTimestamp: pod.GetCreationTimestamp().Time,
							Namespace:         pod.GetNamespace(),
						}
					}
				}
			}
		}
	}

	return podsWithAffinity, nil
}

func isPodReady(pod metav1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return true
		}
	}
	return false
}

func getSpotInstanceIPs(clientset *kubernetes.Clientset) ([]string, error) {
	spotIPs := []string{}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, node := range nodes.Items {
		if node.GetLabels()["cloud.google.com/gke-spot"] == "true" {
			if len(node.Status.Addresses) > 0 {
				spotIPs = append(spotIPs, node.Status.Addresses[0].Address)
			}
		}
	}

	return spotIPs, nil
}

func deleteNonSpotPods(clientset *kubernetes.Clientset, podsWithAffinity map[string]PodWithAffinity, spotIPs []string) error {
	for podName, podInfo := range podsWithAffinity {
		if podInfo.CreationTimestamp.Before(time.Now().Add(-10*time.Minute)) && !slices.Contains(spotIPs, podInfo.HostIP) {
			err := clientset.CoreV1().Pods(podInfo.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete pod %s: %w", podName, err)
			}
			fmt.Printf("Deleted pod %s in namespace %s\n", podName, podInfo.Namespace)
		}
	}
	return nil
}
