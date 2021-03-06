/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package arbitrator

import (
	"fmt"
	"testing"
	"time"

	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/controller"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/preemption"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/proportion"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"github.com/kubernetes-incubator/kube-arbitrator/test/integration/framework"

	"k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

// prepareNamespace prepare three namespaces "ns01" "ns02" "ns03"
func prepareNamespace(cs *clientset.Clientset) error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "xxx",
		},
	}

	cases := []struct {
		name string
	}{
		{
			name: "ns01",
		},
		{
			name: "ns02",
		},
		{
			name: "ns03",
		},
	}

	for _, c := range cases {
		ns.Name = c.name
		_, err := cs.CoreV1().Namespaces().Create(ns)
		if err != nil {
			return fmt.Errorf("fail to create namespace %s, %#v", ns.Name, err)
		}
	}

	return nil
}

// prepareNode prepare one node "node01" which contain 15 cpus and 15Gi memory
func prepareNode(cs *clientset.Clientset) error {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:         "xxx",
			GenerateName: "xxx",
		},
		Spec: v1.NodeSpec{
			ExternalID: "foo",
		},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			},
			Phase: v1.NodeRunning,
			Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue},
			},
		},
	}

	cases := []struct {
		name           string
		resourceCPU    resource.Quantity
		resourceMemory resource.Quantity
	}{
		{
			name:           "node01",
			resourceCPU:    resource.MustParse("15"),
			resourceMemory: resource.MustParse("15Gi"),
		},
	}

	for _, c := range cases {
		node.Name = c.name
		node.GenerateName = c.name
		node.Status.Capacity[v1.ResourceCPU] = c.resourceCPU
		node.Status.Capacity[v1.ResourceMemory] = c.resourceMemory

		_, err := cs.CoreV1().Nodes().Create(node)
		if err != nil {
			return fmt.Errorf("fail to create node %s, %#v", node.Name, err)
		}
	}

	return nil
}

// prepareResourceQuota prepare three resource quotas "rq01" "rq02" "rq03"
// "rq01" is under namespace "ns01"
// "rq02" is under namespace "ns02"
// "rq03" is under namespace "ns03"
// "rq01" "rq02" "rq03" will not limit cpu and memory usage at first
func prepareResourceQuota(cs *clientset.Clientset) error {
	rq := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "xxx",
			Namespace: "xxx",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1000"),
			},
		},
	}

	cases := []struct {
		name string
		ns   string
	}{
		{
			name: "rq01",
			ns:   "ns01",
		},
		{
			name: "rq02",
			ns:   "ns02",
		},
		{
			name: "rq03",
			ns:   "ns03",
		},
	}

	for _, c := range cases {
		rq.Name = c.name
		rq.Namespace = c.ns
		_, err := cs.CoreV1().ResourceQuotas(rq.Namespace).Create(rq)
		if err != nil {
			return fmt.Errorf("fail to create quota %s, %#v", rq.Name, err)
		}
	}

	return nil
}

// prepareCRD prepare customer resource definition "Queue"
// create two Queues, "queue01" and "queue02"
// "queue01" is under namespace "ns01" and has attribute "weight=1"
// "queue02" is under namespace "ns02" and has attribute "weight=2"
func prepareCRD(config *restclient.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("fail to create crd config, %#v", err)
	}

	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("fail to create crd, %#v", err)
	}

	crdClient, _, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("fail to create crd client, %#v", err)
	}

	crd := &apiv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "xxx",
			Namespace: "xxx",
		},
		Spec: apiv1.QueueSpec{
			Weight: 0,
		},
	}

	cases := []struct {
		name   string
		ns     string
		weight int
	}{
		{
			name:   "queue01",
			ns:     "ns01",
			weight: 1,
		},
		{
			name:   "queue02",
			ns:     "ns02",
			weight: 2,
		},
	}

	for _, c := range cases {
		crd.Name = c.name
		crd.Namespace = c.ns
		crd.Spec.Weight = c.weight

		var result apiv1.Queue
		err = crdClient.Post().
			Resource(apiv1.QueuePlural).
			Namespace(crd.Namespace).
			Body(crd).
			Do().Into(&result)
		if err != nil {
			return fmt.Errorf("fail to create crd %s, %#v", crd.Name, err)
		}
	}

	return nil
}

// prepareCRDForPreemption create one Queue "queue03"
// "queue03" is under namespace "ns03" and has attribute "weight=2"
func prepareCRDForPreemption(config *restclient.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("fail to create crd config, %#v", err)
	}

	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("fail to create crd, %#v", err)
	}

	crdClient, _, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("fail to create crd client, %#v", err)
	}

	crd := &apiv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "xxx",
			Namespace: "xxx",
		},
		Spec: apiv1.QueueSpec{
			Weight: 0,
		},
	}

	cases := []struct {
		name   string
		ns     string
		weight int
	}{
		{
			name:   "queue03",
			ns:     "ns03",
			weight: 2,
		},
	}

	for _, c := range cases {
		crd.Name = c.name
		crd.Namespace = c.ns
		crd.Spec.Weight = c.weight

		var result apiv1.Queue
		err = crdClient.Post().
			Resource(apiv1.QueuePlural).
			Namespace(crd.Namespace).
			Body(crd).
			Do().Into(&result)
		if err != nil {
			return fmt.Errorf("fail to create crd %s, %#v", crd.Name, err)
		}
	}

	return nil
}

// preparePods create pods under ns01 and ns02
// 5 pods under ns01
// 8 pods under ns02
// each pod use 1 CPU and 1 Gi Memory
func preparePods(cs *clientset.Clientset) error {
	port := v1.ContainerPort{
		ContainerPort: 6379,
	}
	container := v1.Container{
		Name:  "key-value-store",
		Image: "redis",
		Resources: v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("1Gi"),
			},
			Requests: map[v1.ResourceName]resource.Quantity{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("1Gi"),
			},
		},
		Ports: []v1.ContainerPort{port},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "xxx",
			Namespace: "xxx",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{container},
		},
	}

	cases := []struct {
		name string
		ns   string
	}{
		{
			name: "ns01-pod01",
			ns:   "ns01",
		},
		{
			name: "ns01-pod02",
			ns:   "ns01",
		},
		{
			name: "ns01-pod03",
			ns:   "ns01",
		},
		{
			name: "ns01-pod04",
			ns:   "ns01",
		},
		{
			name: "ns01-pod05",
			ns:   "ns01",
		},
		{
			name: "ns02-pod01",
			ns:   "ns02",
		},
		{
			name: "ns02-pod02",
			ns:   "ns02",
		},
		{
			name: "ns02-pod03",
			ns:   "ns02",
		},
		{
			name: "ns02-pod04",
			ns:   "ns02",
		},
		{
			name: "ns02-pod05",
			ns:   "ns02",
		},
		{
			name: "ns02-pod06",
			ns:   "ns02",
		},
		{
			name: "ns02-pod07",
			ns:   "ns02",
		},
		{
			name: "ns02-pod08",
			ns:   "ns02",
		},
	}

	for _, c := range cases {
		pod.Name = c.name
		pod.Namespace = c.ns

		_, err := cs.CoreV1().Pods(pod.Namespace).Create(pod)
		if err != nil {
			return fmt.Errorf("fail to create pod %s, %#v", pod.Name, err)
		}
	}

	return nil
}

func TestArbitrator(t *testing.T) {
	config, tearDown := framework.StartTestServerOrDie(t)
	defer tearDown()

	cs := clientset.NewForConfigOrDie(config)
	defer cs.CoreV1().Nodes().DeleteCollection(nil, metav1.ListOptions{})

	err := prepareNamespace(cs)
	if err != nil {
		t.Fatalf("fail to prepare namespaces, %#v", err)
	}

	err = prepareNode(cs)
	if err != nil {
		t.Fatalf("fail to prepare node, %#v", err)
	}

	err = prepareResourceQuota(cs)
	if err != nil {
		t.Fatalf("fail to prepare resource quota, %#v", err)
	}

	err = prepareCRD(config)
	if err != nil {
		t.Fatalf("fail to prepare CRD, %#v", err)
	}

	neverStop := make(chan struct{})
	defer close(neverStop)
	cache := schedulercache.New(config)
	go cache.Run(neverStop)
	c := controller.NewQueueController(config, cache, policy.New(proportion.PolicyName), preemption.New(config))
	go c.Run()

	// sleep to wait scheduler finish
	time.Sleep(10 * time.Second)

	// verify scheduler result
	rq01, _ := cs.CoreV1().ResourceQuotas("ns01").Get("rq01", metav1.GetOptions{})
	cpu01 := rq01.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu01).AsInt64(); v != int64(5) {
		t.Fatalf("after scheduler, cpu is not 5 for rq01, %#v", rq01)
	}
	rq02, _ := cs.CoreV1().ResourceQuotas("ns02").Get("rq02", metav1.GetOptions{})
	cpu02 := rq02.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu02).AsInt64(); v != int64(10) {
		t.Fatalf("after scheduler, cpu is not 10 for rq02, %#v", rq02)
	}

	// test preemption
	err = preparePods(cs)
	if err != nil {
		t.Fatalf("fail to prepare Pods, %#v", err)
	}
	// sleep to wait pods creation done
	time.Sleep(10 * time.Second)
	pods01, _ := cs.CoreV1().Pods("ns01").List(metav1.ListOptions{})
	if len(pods01.Items) != 5 {
		t.Fatalf("running pods size is not 5 for ns01, %#v", pods01.Items)
	}
	pods02, _ := cs.CoreV1().Pods("ns02").List(metav1.ListOptions{})
	if len(pods02.Items) != 8 {
		t.Fatalf("running pods size is not 8 for ns02, %#v", pods02.Items)
	}

	// create a new queue to trigger preemption
	err = prepareCRDForPreemption(config)
	if err != nil {
		t.Fatalf("fail to prepare CRD for preemption, %#v", err)
	}

	// sleep to wait scheduler finish
	time.Sleep(20 * time.Second)
	rq01, _ = cs.CoreV1().ResourceQuotas("ns01").Get("rq01", metav1.GetOptions{})
	cpu01 = rq01.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu01).AsInt64(); v != int64(3) {
		t.Fatalf("after preemption, cpu is not 3 for rq01, %#v", rq01)
	}
	rq02, _ = cs.CoreV1().ResourceQuotas("ns02").Get("rq02", metav1.GetOptions{})
	cpu02 = rq02.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu02).AsInt64(); v != int64(6) {
		t.Fatalf("after preemption, cpu is not 6 for rq02, %#v", rq02)
	}
	rq03, _ := cs.CoreV1().ResourceQuotas("ns03").Get("rq03", metav1.GetOptions{})
	cpu03 := rq03.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu03).AsInt64(); v != int64(6) {
		t.Fatalf("after preemption, cpu is not 6 for rq03, %#v", rq03)
	}
	pods01, _ = cs.CoreV1().Pods("ns01").List(metav1.ListOptions{})
	if len(pods01.Items) != 3 {
		t.Fatalf("after preemption, pods size is not 3 for ns01, %#v", pods01.Items)
	}
	pods02, _ = cs.CoreV1().Pods("ns02").List(metav1.ListOptions{})
	if len(pods02.Items) != 6 {
		t.Fatalf("after preemption, pods size is not 6 for ns02, %#v", pods02.Items)
	}
}
