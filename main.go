package main

import (
	"context"
	"flag"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	pods_custom            = make(map[string]map[string]string)
	list_of_spot_instances = make(map[string]map[string]string)
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
	for _, pod := range pods.Items {
		pods_custom[pod.GetName()] = map[string]string{"HostIP": pod.Status.HostIP, "Spot": pod.Labels["spot"]}
	}

	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})

	for _, node := range nodes.Items {
		if node.GetLabels()["is_spot"] == "true" {
			fmt.Println("The node is a spot instance -> ", node.GetName())
			list_of_spot_instances[node.GetName()] = map[string]string{"HostIP": node.Status.Addresses[0].Address}
		}
	}

	fmt.Println(list_of_spot_instances)

	for k, v := range pods_custom {
		for _, v1 := range list_of_spot_instances {
			if v["HostIP"] == v1["HostIP"] {
				fmt.Println("The pod should be deleted and put on spot -> ", k)
			}
		}
	}
}
