# kubernetes-descheduler

## Reason for this descheduler - Cost Saving in GKE

When using a GKE cluster with spot instances, we usually use node affinities for workloads that are tolerant to failure and put them on spot instances, for cost reduction. 

The node affinity looks something like this:

```apiVersion: v1
kind: Pod
metadata:
  name: with-node-affinity
spec:
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 1
        preference:
          matchExpressions:
          - key: cloud.google.com/gke-spot
            operator: In
            values:
            - true
  containers:
  - name: with-node-affinity
    image: registry.k8s.io/pause:3.8
```

We use preferredDuringSchedulingIgnoredDuringExecution and not RequiredDuringSchedulingIgnoredDuringExecution to allow the pods on spot instances to be put on the on-demand instances in case google decides to take back the spot instances.

In case if Google decides to kill a spot instance, it drains and puts the "should be running on spot instance" pods on on-demand nodes to continue the work. 

However, when a new spot instance is created as a replacement of the older one, the pods that "should be running on spot instance" and which ones were put on the on-demand nodes are not automatically scheduled on the spot instances and continue running and using compute of the on-demand instances. Hence increasing the price. 

## How does the descheduler work

NOTE: The descheduler should be running as a cronjob that can run once a day. 

1. The Descheduler finds all the pods and records the metadata of such pods that have a node affinity to the GKE spot nodes.

The recorded metadata includes:
 - Namespace
 - Host IP on which the pod is running
 - Node Name on which the pod is running
 - Pod Creation Timestamp

2. It creates a list of IP addresses of all the nodes that are GKE spot instances, based on the node label cloud.google.com/gke-spot=true. GKE automatically adds this label on all the spot instances in a cluster. 

3. Then, the descheduler runs the pods and sees if the pod's host IP is in the list that is created in step 2. If it is found, the pod that was supposed to be running on a spot instance, is already running on a spot instance. If not, it deletes the pod and because of the affinity rules, the pod is put on one of the spot instances.


The descheduler is idempotent. In a way that if it does not find a pod that needs to be put on a spot instance, or in other words, if all the pods that are supposed to be running on a spot instance ARE indeed running on spot instances, it does not do anything. 