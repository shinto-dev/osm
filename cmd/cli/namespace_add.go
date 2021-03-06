package main

import (
	"context"
	"fmt"
	"io"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/openservicemesh/osm/pkg/constants"
)

const namespaceAddDescription = `
This command will add a namespace or a set of namespaces
to the mesh. Optionally, the namespaces can be configured
for automatic sidecar injection which enables pods in the
added namespaces to be injected with a sidecar upon creation.
`

type namespaceAddCmd struct {
	out                    io.Writer
	namespaces             []string
	meshName               string
	enableSidecarInjection bool
	clientSet              kubernetes.Interface
}

func newNamespaceAdd(out io.Writer) *cobra.Command {
	namespaceAdd := &namespaceAddCmd{
		out: out,
	}

	cmd := &cobra.Command{
		Use:   "add NAMESPACE ...",
		Short: "add namespace to mesh",
		Long:  namespaceAddDescription,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			namespaceAdd.namespaces = args
			config, err := settings.RESTClientGetter().ToRESTConfig()
			if err != nil {
				return errors.Errorf("Error fetching kubeconfig")
			}

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return errors.Errorf("Could not access Kubernetes cluster. Check kubeconfig")
			}
			namespaceAdd.clientSet = clientset
			return namespaceAdd.run()
		},
	}

	//add mesh name flag
	f := cmd.Flags()
	f.StringVar(&namespaceAdd.meshName, "mesh-name", "osm", "Name of the service mesh")
	f.BoolVar(&namespaceAdd.enableSidecarInjection, "enable-sidecar-injection", false, "Enable automatic sidecar injection")

	return cmd
}

func (a *namespaceAddCmd) run() error {
	for _, ns := range a.namespaces {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		deploymentsClient := a.clientSet.AppsV1().Deployments(ns)
		labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": constants.OSMControllerName}}

		listOptions := metav1.ListOptions{
			LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		}
		list, _ := deploymentsClient.List(context.TODO(), listOptions)

		// if osm-controller is installed in this namespace then don't add that to mesh
		if len(list.Items) != 0 {
			fmt.Fprintf(a.out, "Namespace [%s] already has [%s] installed and cannot be added to mesh [%s]\n", ns, constants.OSMControllerName, a.meshName)
		} else {
			var patch string
			if a.enableSidecarInjection {
				// Patch the namespace with the monitoring label and sidecar injection annotation
				patch = fmt.Sprintf(`
{
	"metadata": {
		"labels": {
			"%s": "%s"
		},
		"annotations": {
			"%s": "enabled"
		}
	}
}`, constants.OSMKubeResourceMonitorAnnotation, a.meshName, constants.SidecarInjectionAnnotation)
			} else {
				// Patch the namespace with monitoring label
				patch = fmt.Sprintf(`
{
	"metadata": {
		"labels": {
			"%s": "%s"
		}
	}
}`, constants.OSMKubeResourceMonitorAnnotation, a.meshName)
			}

			_, err := a.clientSet.CoreV1().Namespaces().Patch(ctx, ns, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}, "")
			if err != nil {
				return errors.Errorf("Could not add namespace [%s] to mesh [%s]: %v", ns, a.meshName, err)
			}

			fmt.Fprintf(a.out, "Namespace [%s] successfully added to mesh [%s]\n", ns, a.meshName)
		}
	}

	return nil
}
