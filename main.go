package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
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
		panic(err.Error())
	}

	pods, err := podGetter(clientset)
	if err != nil {
		fmt.Println(err)
		return
	}

	podsWithAffinity := filterPodsWithAffinity(pods)
	if len(podsWithAffinity) == 0 {
		fmt.Print("No pods with affinity.")
		return
	}

	nodes, err := NodeGetter(clientset)
	if err != nil {
		fmt.Println(err)
		return
	}

	spotIPs := getSpotInstanceIPs(nodes)
	if len(spotIPs) == 0 {
		fmt.Println("No spot instances are available. Exiting.")
		return
	}

	podsDeleted, err := deleteNonSpotPods(clientset, podsWithAffinity, spotIPs)
	if err != nil {
		fmt.Println("failed to delete non-spot pods: %w", err)
		return
	}

	for _, podName := range podsDeleted {
		fmt.Println("Pod", podName, "scheduled on a spot node.")
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

func podGetter(clientset *kubernetes.Clientset) (*v1.PodList, error) {
	return clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
}

func NodeGetter(clientset *kubernetes.Clientset) (*v1.NodeList, error) {
	return clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
}

func filterPodsWithAffinity(pods *v1.PodList) map[string]PodWithAffinity {

	podsWithAffinity := make(map[string]PodWithAffinity)

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
	return podsWithAffinity
}

func isPodReady(pod v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			return true
		}
	}
	return false
}

func getSpotInstanceIPs(nodes *v1.NodeList) []string {
	spotIPs := []string{}

	for _, node := range nodes.Items {
		if node.GetLabels()["cloud.google.com/gke-spot"] == "true" {
			if len(node.Status.Addresses) > 0 {
				spotIPs = append(spotIPs, node.Status.Addresses[0].Address)
			}
		}
	}
	return spotIPs
}

func deleteNonSpotPods(clientset *kubernetes.Clientset, podsWithAffinity map[string]PodWithAffinity, spotIPs []string) ([]string, error) {
	podsDeleted := []string{}
	for podName, podInfo := range podsWithAffinity {
		if podInfo.CreationTimestamp.Before(time.Now().Add(10*time.Minute)) && !slices.Contains(spotIPs, podInfo.HostIP) {
			err := clientset.CoreV1().Pods(podInfo.Namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
			if err != nil {
				return podsDeleted, fmt.Errorf("failed to delete pod %s: %w", podName, err)
			}
			podsDeleted = append(podsDeleted, podName)
		}
	}
	return podsDeleted, nil
}
