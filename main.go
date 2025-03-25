package main

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type PodName = string
type NodeName = string

var (
	pods_with_affinity    = make(map[PodName]PodWithAffinity)
	map_of_spot_instances = make(map[NodeName]SpotInstance)
	list_of_spot_ips      = make([]string, 0)
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

	kubeconfig := flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	// find pods that have a node with affinity to spot instances
	for _, pod := range pods.Items {

		if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
			for _, term := range pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
				for _, expr := range term.Preference.MatchExpressions {
					if expr.Key == "cloud.google.com/gke-spot" && slices.Contains(expr.Values, "true") {
						fmt.Printf("Pod %s has node affinity to spot instances\n", pod.GetName())
						// check if pod is actually Ready by looping over status.conditions
						for _, condition := range pod.Status.Conditions {
							if condition.Type == "Ready" {
								if condition.Status == "True" {
									pods_with_affinity[pod.GetName()] = PodWithAffinity{HostIP: pod.Status.HostIP, CreationTimestamp: pod.GetCreationTimestamp().Time, Namespace: pod.GetNamespace()}
								}
							}
						}
					}
				}
			}
		}
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	for _, node := range nodes.Items {
		if node.GetLabels()["cloud.google.com/gke-spot"] == "true" {
			map_of_spot_instances[node.GetName()] = SpotInstance{HostIP: node.Status.Addresses[0].Address}
		}
	}

	// convert map_of_spot_instances to slice
	for _, v := range map_of_spot_instances {
		list_of_spot_ips = append(list_of_spot_ips, v.HostIP)
	}

	for k, podinfo := range pods_with_affinity {
		if podinfo.CreationTimestamp.Before(time.Now().Add(10 * time.Minute)) {
			if !slices.Contains(list_of_spot_ips, podinfo.HostIP) {
				err = clientset.CoreV1().Pods(podinfo.Namespace).Delete(context.TODO(), k, metav1.DeleteOptions{})
				if err != nil {
					panic(err.Error())
				}
			}
		}
	}

	finish := time.Now()
	fmt.Printf("Execution time: %v\n", finish.Sub(start))

}
