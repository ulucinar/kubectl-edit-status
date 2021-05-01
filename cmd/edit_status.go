// HO.

/*
Copyright 2020 Alper Rifat Ulucinar

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

/*
This software is a derivative work based upon:
- https://github.com/mstrzele/helm-edit
  [License: LICENSE.helm-edit]
- https://github.com/kubernetes/sample-cli-plugin
  [License: LICENSE.sample-cli-plugin]

Modifications:
- Declare EditStatusOptions
- Implement cmd.NewCmdEditStatus, which provides the "edit status" command
*/

/*
Copyright 2018 The Kubernetes Authors.
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

package cmd

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"

	"fmt"

	jsonPatch "github.com/evanphx/json-patch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	argEditStatus = "edit-status"

	envEditor     = "EDITOR"
	envKubeEditor = "KUBE_EDITOR"

	delimEditorNames = ":"

	defaultEditorName = "vi"

	patternTmpFile = "kubectl-edit-status-"

	editStatusExample = `
	# edit the status field of the MyResource CR named "test", which uses status subresource 
	kubectl %s myresource test
`
)

// EditStatusOptions provides information required to update
// the current context on a user's KUBECONFIG
type EditStatusOptions struct {
	configFlags *genericclioptions.ConfigFlags

	namespaced     bool
	namespace      string
	resource       string
	resourceName   string
	resourceEditor string

	dynamicClient   dynamic.Interface
	restConfig      *rest.Config
	discoveryClient discovery.CachedDiscoveryInterface
	restMapper      meta.RESTMapper

	gvk          schema.GroupVersionKind
	originalJSon []byte

	genericclioptions.IOStreams

	filePath string
}

// NewEditStatusOptions provides an instance of EditStatusOptions with default values
func NewEditStatusOptions(streams genericclioptions.IOStreams) *EditStatusOptions {
	return &EditStatusOptions{
		configFlags: genericclioptions.NewConfigFlags(true),

		IOStreams: streams,
	}
}

// NewCmdEditStatus provides a cobra command wrapping EditStatusOptions
func NewCmdEditStatus(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewEditStatusOptions(streams)

	cmd := &cobra.Command{
		Use:          fmt.Sprintf("kubectl %s [resource] [resource-name] [flags]", argEditStatus),
		Short:        "Edit /status subresource",
		Example:      fmt.Sprintf(editStatusExample, argEditStatus),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(cmd, args); err != nil {
				if usageErr := cmd.Usage(); usageErr != nil {
					// log usageError
					_, _ = fmt.Fprintf(o.ErrOut, "Error occured while printing command usage: %s", usageErr.Error())
				}

				return err
			}

			return nil
		},
		RunE: o.Run,
	}

	cmd.Flags().BoolVar(&o.namespaced, "namespaced", true, "set to false for cluster-scoped resources")
	cmd.Flags().StringVarP(&o.resourceEditor, "editor", "e",
		fmt.Sprintf("${%s}%s${%s}%s%s",
			envKubeEditor, delimEditorNames, envEditor, delimEditorNames, defaultEditorName),
		fmt.Sprintf("editor to use. Either editor name in PATH or path to the editor executable. "+
			"If not specified, first value of %q and then value of %q environment variables are substituted and checked",
			envKubeEditor, envEditor))
	cmd.Flags().StringVarP(&o.filePath, "in", "i", "", "Edit status of the resource with specified yaml file")

	// add K8s generic client flags
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// Validate ensures that all required arguments and flag values are provided and fills the EditStatusOptions receiver arg
func (o *EditStatusOptions) Validate(cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("not enough arguments")
	}

	o.resource = args[0]
	o.resourceName = args[1]

	var err error

	o.discoveryClient, err = o.configFlags.ToDiscoveryClient()

	if err != nil {
		return err
	}

	o.restConfig, err = o.configFlags.ToRESTConfig()

	if err != nil {
		return err
	}

	o.dynamicClient, err = dynamic.NewForConfig(o.restConfig)

	if err != nil {
		return err
	}

	o.restMapper, err = o.configFlags.ToRESTMapper()

	if err != nil {
		return err
	}

	o.namespace, err = cmd.Flags().GetString("namespace")

	if err != nil {
		return err
	}

	return o.storeEditorPath()
}

func (o *EditStatusOptions) storeEditorPath() error {
	editorNames := strings.Split(os.ExpandEnv(o.resourceEditor), delimEditorNames)

	o.resourceEditor = ""

	for _, e := range editorNames {
		trimmedName := strings.TrimSpace(e)

		if trimmedName != "" {
			o.resourceEditor = trimmedName

			break
		}
	}

	if o.resourceEditor == "" {
		return fmt.Errorf("resource editor not specified")
	}

	return nil
}

// Run forks an editor for editing the specified CR's status subresource
func (o *EditStatusOptions) Run(_ *cobra.Command, _ []string) (err error) {
	tmpEditFile, err := ioutil.TempFile(os.TempDir(), patternTmpFile)

	if err != nil {
		return err
	}

	defer func() {
		errRemove := os.Remove(tmpEditFile.Name())

		if err == nil {
			err = errRemove
		} else if errRemove != nil {
			_, _ = fmt.Fprintf(o.ErrOut, "Error occured while removing temporary file %q: %s",
				tmpEditFile.Name(), errRemove.Error())
		}
	}()

	if err = o.storeResource(tmpEditFile); err != nil {
		return
	}

	if o.filePath != "" {
		if err = o.writeResourceStatusFromInputFile(); err != nil {
			return
		}

		return nil
	}

	if err = o.editResource(tmpEditFile); err != nil {
		return
	}

	if err = o.writeResourceStatus(tmpEditFile); err != nil {
		return
	}

	return nil
}

func (o *EditStatusOptions) storeResource(f *os.File) error {
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(o.discoveryClient)
	resource := schema.GroupVersionResource{
		Resource: o.resource,
	}

	gvrs, err := restMapper.ResourcesFor(resource)

	if err != nil {
		return err
	}

	for i, gvr := range gvrs {
		var obj *unstructured.Unstructured
		var ri dynamic.ResourceInterface = o.dynamicClient.Resource(gvr)

		if o.namespaced {
			ri = ri.(dynamic.NamespaceableResourceInterface).Namespace(o.namespace)
		}

		if obj, err = ri.Get(context.TODO(),
			o.resourceName, metaV1.GetOptions{}); errors.IsNotFound(err) && i != len(gvrs)-1 {
			// then resource with given GVR is not found and there are more gvrs to try
			continue
		} else if err == nil {
			if o.originalJSon, err = obj.MarshalJSON(); err != nil {
				return err
			} else if buff, err := yaml.JSONToYAML(o.originalJSon); err != nil {
				return err
			} else if _, err = f.Write(buff); err != nil {
				return err
			} else if err = f.Sync(); err != nil {
				return err
			} else if o.gvk, err = o.restMapper.KindFor(gvr); err != nil {
				return err
			}

			return nil
		} else {
			return err
		}
	}

	return fmt.Errorf("resource %s %q not found", o.resource, o.resourceName)
}

func (o *EditStatusOptions) editResource(f *os.File) error {
	cmd := exec.Command(o.resourceEditor, f.Name())

	cmd.Stdin = o.In
	cmd.Stdout = o.Out
	cmd.Stderr = o.ErrOut

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func (o *EditStatusOptions) writeResourceStatus(f *os.File) error {
	restMapping, err := o.restMapper.RESTMapping(o.gvk.GroupKind(), o.gvk.Version)

	if err != nil {
		return err
	}

	editedYaml, err := ioutil.ReadFile(f.Name())

	if err != nil {
		return err
	}

	editedJSon, err := yaml.YAMLToJSON(editedYaml)

	if err != nil {
		return err
	}

	patch, err := jsonPatch.CreateMergePatch(o.originalJSon, editedJSon)

	if err != nil {
		return err
	}

	restClient, err := apiutil.RESTClientForGVK(o.gvk, o.restConfig, serializer.CodecFactory{})

	if err != nil {
		return err
	}

	_, err = restClient.Patch(types.MergePatchType).
		NamespaceIfScoped(o.namespace, restMapping.Scope.Name() == meta.RESTScopeNameNamespace).
		Resource(restMapping.Resource.Resource).
		Name(o.resourceName).
		SubResource("status").
		VersionedParams(&metaV1.PatchOptions{}, metaV1.ParameterCodec).
		Body(patch).
		DoRaw(context.TODO())

	if err != nil {
		return err
	}

	return nil
}

func (o *EditStatusOptions) writeResourceStatusFromInputFile() error {
	f, err := os.Open(o.filePath)

	if err != nil {
		return err
	}

	defer func() {
		if errClose := f.Close(); err != nil {
			err = errClose
		}
	}()

	if err = o.writeResourceStatus(f); err != nil {
		return err
	}

	return nil
}
