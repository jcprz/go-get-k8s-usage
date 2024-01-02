package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

const (
    colorRed   = "\033[31m"
    colorGreen = "\033[32m"
	colorYellow = "\033[33m"
    colorReset = "\033[0m"
)

func main() {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	metricsClientset, err := versioned.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Initialize tabwriter
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer writer.Flush()

	// Print header
	fmt.Fprintln(writer, "POD\tCONTAINER\tUSAGE\tREQUESTS\tLIMITS")

	for _, pod := range pods.Items {
		  // Skip pods in the kube-system namespace
		  if pod.Namespace == "kube-system" {
			continue
		}
	
		// Get metrics for the pod
		metrics, err := metricsClientset.MetricsV1beta1().PodMetricses(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			fmt.Fprintf(writer, "Error getting metrics for pod %s: %v\n", pod.Name, err)
			continue
		}

		for i, container := range pod.Spec.Containers {
			if i < len(metrics.Containers) {
				usage := metrics.Containers[i].Usage[v1.ResourceMemory]
				requests := container.Resources.Requests[v1.ResourceMemory]
				limits := container.Resources.Limits[v1.ResourceMemory]
		
				usageStr := compareAndColorUsage(usage, requests)
				requestsStr := colorYellow + formatResourceQuantity(requests) + colorReset
				limitsStr := colorYellow + formatResourceQuantity(limits) + colorReset
		
				fmt.Fprintf(writer, colorYellow+"%s\t%s\t"+colorReset+"%s\t%s\t%s\n", pod.Name, container.Name, usageStr, requestsStr, limitsStr)
			}
		}
		
		
	}
}

func formatResourceQuantity(q resource.Quantity) string {
	if q.IsZero() {
		return "Not Set"
	}

	// Convert to bytes and then to MiB
	bytes := q.Value()
	mib := bytes / (1024 * 1024)
	if mib > 0 {
		return fmt.Sprintf("%dMi", mib)
	}

	// If the value is less than 1 MiB, show it in KiB
	kib := bytes / 1024
	if kib > 0 {
		return fmt.Sprintf("%dKi", kib)
	}

	return "0Mi"
}

func compareAndColorUsage(usage, request resource.Quantity) string {
    // Compare the numeric values of usage and request
    if usage.Cmp(request) > 0 {
        // If usage is greater than request, color it red
        return colorRed + formatResourceQuantity(usage) + colorReset
    }
    // Else color it green
    return colorGreen + formatResourceQuantity(usage) + colorReset
}


func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}
