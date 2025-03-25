package main

import (
	"context"
	"flag"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	pods_custom           = make(map[string]map[string]string)
	map_of_spot_instances = make(map[string]map[string]string)
	list_of_spot_ips      = make([]string, 0)
)

func main() {
	var kubeconfig *string

	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	// find pods that have a node with affinity to spot instances
	for _, pod := range pods.Items {
		if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
			for _, term := range pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
				for _, expr := range term.Preference.MatchExpressions {
					if expr.Key == "cloud.google.com/gke-spot" && slices.Contains(expr.Values, "true") {
						fmt.Printf("Pod %s has node affinity to spot instances\n", pod.GetName())
						pods_custom[pod.GetName()] = map[string]string{"HostIP": pod.Status.HostIP, "NodeName": pod.Spec.NodeName, "CreationTimestamp": pod.GetCreationTimestamp().String(), "Namespace": pod.GetNamespace()}
					}
				}
			}
		}
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	for _, node := range nodes.Items {
		if node.GetLabels()["cloud.google.com/gke-spot"] == "true" {
			map_of_spot_instances[node.GetName()] = map[string]string{"HostIP": node.Status.Addresses[0].Address}
		}
	}

	// convert map_of_spot_instances to slice
	for _, v := range map_of_spot_instances {
		list_of_spot_ips = append(list_of_spot_ips, v["HostIP"])
	}

	for k, podinfo := range pods_custom {
		if !slices.Contains(list_of_spot_ips, podinfo["HostIP"]) {
			err = clientset.CoreV1().Pods(podinfo["Namespace"]).Delete(context.TODO(), k, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
		}
	}
}
