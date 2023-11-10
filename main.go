// Based on https://stackoverflow.com/a/74501528
package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func writeYaml(path string, obj runtime.Object) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil)
	return encoder.Encode(obj, f)
}

func diff(oldObj runtime.Object, newObj runtime.Object) error {
	err := writeYaml("/tmp/obj1.yaml", oldObj)
	if err != nil {
		return err
	}

	err = writeYaml("/tmp/obj2.yaml", newObj)
	if err != nil {
		return err
	}

	cmd := exec.Command("/usr/bin/git", "--no-pager", "diff", "--no-index", "/tmp/obj1.yaml", "/tmp/obj2.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
	return nil
}

func main() {
	kubeconfig := flag.String("kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "absolute path to the kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	// stop signal for the informer
	stopper := make(chan struct{})
	defer close(stopper)

	// create shared informers for resources in all known API group versions with a reSync period and namespace
	factory := informers.NewSharedInformerFactoryWithOptions(clientSet, 10*time.Second, informers.WithNamespace("default"))
	podInformer := factory.Core().V1().Pods().Informer()
	deploymentInformer := factory.Apps().V1().Deployments().Informer()

	defer utilRuntime.HandleCrash()

	// start informer
	go factory.Start(stopper)

	// start to sync and call list
	if !cache.WaitForCacheSync(stopper, podInformer.HasSynced) {
		log.Fatal("timed out waiting for caches to sync")
	}
	if !cache.WaitForCacheSync(stopper, deploymentInformer.HasSynced) {
		log.Fatal("timed out waiting for caches to sync")
	}

	/*podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			log.Printf("POD CREATED: %s/%s", pod.Namespace, pod.Name)
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			oldPod := oldObj.(*corev1.Pod)
			newPod := newObj.(*corev1.Pod)
			log.Printf("POD MODIFIED: %s/%s", newPod.Namespace, newPod.Name)
			diff(oldPod, newPod)
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			log.Printf("POD DELETED: %s/%s\n", pod.Namespace, pod.Name)
		},
	})*/
	deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			dep := obj.(*appsv1.Deployment)
			log.Printf("DEPLOYMENT CREATED: %s/%s", dep.Namespace, dep.Name)
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			oldDep := oldObj.(*appsv1.Deployment)
			newDep := newObj.(*appsv1.Deployment)

			oldDep.Status = appsv1.DeploymentStatus{}
			newDep.Status = appsv1.DeploymentStatus{}
			if !reflect.DeepEqual(oldDep, newDep) {
				log.Printf("DEPLOYMENT MODIFIED: %s/%s", newDep.Namespace, newDep.Name)
				diff(oldDep, newDep)
			}
		},
		DeleteFunc: func(obj interface{}) {
			dep := obj.(*appsv1.Deployment)
			log.Printf("DEPLOYMENT DELETED: %s/%s\n", dep.Namespace, dep.Name)
		},
	})

	// block the main go routine from exiting
	<-stopper
}
