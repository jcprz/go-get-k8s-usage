package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

var (
	kubeconfig string
	kubeContext string

)

const (
    colorRed   = "\033[31m"
    colorGreen = "\033[32m"
	colorYellow = "\033[33m"
    colorReset = "\033[0m"
)

func main() {
    home := homeDir()

	var rootCmd = &cobra.Command{Use: "kpulse"}

    defaultKubeconfig := filepath.Join(home, ".kube", "config")
    rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig, "Path to the kubeconfig file")
    rootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")

	rootCmd.AddCommand(usageCmd())
	rootCmd.AddCommand(pVCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	
}

func usageCmd() *cobra.Command {
	var namespace string
	var usageCmd = &cobra.Command{
		Use:   "usage",
		Short: "Check CPU and memory usage",
		Run: func(cmd *cobra.Command, args []string) {
			_, clientset, metricsClientset := getClientSets()
			listOptions := metav1.ListOptions{}
			if namespace != "" {
				listOptions.FieldSelector = "metadata.namespace=" + namespace
			}
			pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), listOptions)
			if err != nil {
				panic(err.Error())
			}


		// Filter out non-running pods
		runningPods := filterRunningPods(pods.Items)

		// Initialize progress bar
		bar := pb.Full.Start(len(runningPods))
	

		// Initialize tabwriter
		writer := tabwriter.NewWriter(os.Stdout, 0, 8, 2, '\t', 0)
		defer writer.Flush()


		fmt.Fprintln(writer, "\nPOD\tCONTAINER\tUSAGE\tREQUESTS\tLIMITS")

			// var podDetails []string


			for _, pod := range runningPods {
				if pod.Namespace == "kube-system" {
					bar.Increment()
					continue
				}
	
				metrics, err := metricsClientset.MetricsV1beta1().PodMetricses(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
				if err != nil {
					// Handle missing metrics more gracefully
					fmt.Printf("Error getting metrics for pod %s: %v\n", pod.Name, err)
					bar.Increment()
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
						// podDetails = append(podDetails, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", pod.Name, container.Name, usageStr, requestsStr, limitsStr))

						fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", pod.Name, container.Name, usageStr, requestsStr, limitsStr)
					}
				}
				bar.Increment()

			}
			bar.Finish()
		},
	}
	usageCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Specify the namespace (default is all namespaces)")
	return usageCmd
}

func filterRunningPods(pods []v1.Pod) []v1.Pod {
	var runningPods []v1.Pod
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			runningPods = append(runningPods, pod)
		}
	}
	return runningPods
}

func pVCmd() *cobra.Command {
    var pvCmd = &cobra.Command{
        Use:   "pv",
        Short: "Check PV and PVC details",
        Run: func(cmd *cobra.Command, args []string) {
            _, clientset, _ := getClientSets()

            pvs, err := clientset.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
            if err != nil {
                panic(err.Error())
            }

            nodePVCount := make(map[string]int)
            for _, pv := range pvs.Items {
                nodes := getNodeAffinityNodes(pv)
                for _, node := range nodes {
                    nodePVCount[node]++
                }
            }

            // Initialize tabwriter
            writer := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', tabwriter.AlignRight)
            defer writer.Flush()

            // Print header using tabwriter
            fmt.Fprintln(writer, "PV\tNODE AFFINITY\tCLAIMNAMESPACE\tCLAIMNAME")

            for _, pv := range pvs.Items {
                nodeAffinity := getNodeAffinity(pv, nodePVCount)
                claimNamespace, claimName := getClaimDetails(pv)

                // Write details using tabwriter
                fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", pv.Name, nodeAffinity, claimNamespace, claimName)
            }

            // Flush to print the tabulated data
            writer.Flush()
        },
    }

    return pvCmd
}

func getNodeAffinityNodes(pv v1.PersistentVolume) []string {
    var nodes []string
    if pv.Spec.NodeAffinity != nil &&
       pv.Spec.NodeAffinity.Required != nil &&
       len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms) > 0 {
        for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
            for _, expression := range term.MatchExpressions {
                nodes = append(nodes, expression.Values...)
            }
        }
    }
    return nodes
}
func getNodeAffinity(pv v1.PersistentVolume, nodePVCount map[string]int) string {
    nodes := getNodeAffinityNodes(pv)
    var nodeSelectors []string
    for _, node := range nodes {
        color := colorGreen
        if nodePVCount[node] > 1 {
            color = colorRed
        }
        nodeSelectors = append(nodeSelectors, color + node + colorReset)
    }
    return strings.Join(nodeSelectors, ", ")
}

func getClaimDetails(pv v1.PersistentVolume) (string, string) {
    if pv.Spec.ClaimRef != nil {
        return pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name
    }
    return "Not Bound", "Not Bound"
}

func getClientSets() (*rest.Config, *kubernetes.Clientset, *versioned.Clientset) {
    // Explicitly print the kubeconfig file being used for debugging
    // fmt.Printf("Using kubeconfig file: %s\n", kubeconfig)

    config, err := clientcmd.LoadFromFile(kubeconfig)
    if err != nil {
        panic(fmt.Errorf("failed to load kubeconfig file: %w", err))
    }

    

    // Debug print to show all contexts in the kubeconfig file
    // for contextName := range config.Contexts {
    //     fmt.Println("Found context:", contextName)
    // }

    if kubeContext != "" {
        if _, exists := config.Contexts[kubeContext]; !exists {
            panic(fmt.Errorf("context %q does not exist in the kubeconfig file", kubeContext))
        }
        config.CurrentContext = kubeContext
    }

	fmt.Println("Using current context:", config.CurrentContext)


    restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
    if err != nil {
        panic(fmt.Errorf("failed to create client config: %w", err))
    }

    clientset, err := kubernetes.NewForConfig(restConfig)
    if err != nil {
        panic(fmt.Errorf("failed to create Kubernetes clientset: %w", err))
    }

    metricsClientset, err := versioned.NewForConfig(restConfig)
    if err != nil {
        panic(fmt.Errorf("failed to create metrics clientset: %w", err))
    }

    return restConfig, clientset, metricsClientset
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
