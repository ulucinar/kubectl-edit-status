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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	jsonPatch "github.com/evanphx/json-patch"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

const (
	argEditStatus = "edit-status"

	envEditor     = "EDITOR"
	envKubeEditor = "KUBE_EDITOR"

	delimEditorNames = ":"

	defaultEditorName = "vi"

	patternTmpFile = "kubectl-edit-status-"

	fmtUsage = `	kubectl %s [flags] <partial resource specification> <resource name>
	or,
	kubectl %s [flags] <partial resource specification>/<resource name>
`
	fmtEditStatusExample = `	# edit the status field of the MyResource CR named "test", which uses status subresource 
	kubectl %s myresource test
	kubectl %s myresource/test
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
}

// NewEditStatusOptions provides an instance of EditStatusOptions with default values
func NewEditStatusOptions(streams genericclioptions.IOStreams) *EditStatusOptions {
	return &EditStatusOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

// NewCmdEditStatus provides a cobra command wrapping EditStatusOptions
func NewCmdEditStatus(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewEditStatusOptions(streams)
	cmd := &cobra.Command{
		Use:          fmt.Sprintf(fmtUsage, argEditStatus, argEditStatus),
		Short:        "Edit /status subresource",
		Example:      fmt.Sprintf(fmtEditStatusExample, argEditStatus, argEditStatus),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Init(cmd, args); err != nil {
				if usageErr := cmd.Usage(); usageErr != nil {
					// log usageError
					_, _ = fmt.Fprintf(o.ErrOut, "Error occured while printing command usage: %s", usageErr.Error())
				}
				return errors.Wrap(err, "cannot initialize")
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

	// add K8s generic client flags
	o.configFlags.AddFlags(cmd.Flags())
	return cmd
}

// Init ensures that all required arguments and flag values are provided and fills the EditStatusOptions receiver arg
func (o *EditStatusOptions) Init(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 2:
		o.resource = args[0]
		o.resourceName = args[1]
	case 1:
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return errors.New("single command-line argument must be in the following format: <partial resource specification>/<resource name>")
		}
		o.resource = parts[0]
		o.resourceName = parts[1]
	default:
		return errors.New("invalid number of command-line arguments. Expecting 1 or 2 arguments.")
	}

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
		return errors.New("resource editor not specified")
	}
	return nil
}

// Run forks an editor for editing the specified CR's status subresource
func (o *EditStatusOptions) Run(_ *cobra.Command, _ []string) error {
	tmpEditFile, err := ioutil.TempFile(os.TempDir(), patternTmpFile)
	if err != nil {
		return errors.Wrap(err, "cannot create temp file")
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

	if err := o.storeResource(tmpEditFile); err != nil {
		return err
	}
	if err := o.editResource(tmpEditFile); err != nil {
		return err
	}
	return o.writeResourceStatus(tmpEditFile)
}

func (o *EditStatusOptions) storeResource(f *os.File) error {
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(o.discoveryClient)
	resourceParts := strings.Split(o.resource, ".")
	gvr := schema.GroupVersionResource{
		Resource: resourceParts[0],
		Group:    strings.Join(resourceParts[1:], "."),
	}
	gvrs, err := restMapper.ResourcesFor(gvr)
	if err != nil {
		return errors.Wrapf(err, "cannot get GVRs for partial specification: %s", gvr.String())
	}
	for _, gvr := range gvrs {
		var obj *unstructured.Unstructured
		var ri dynamic.ResourceInterface = o.dynamicClient.Resource(gvr)
		if o.namespaced {
			ri = ri.(dynamic.NamespaceableResourceInterface).Namespace(o.namespace)
		}
		if obj, err = ri.Get(context.TODO(),
			o.resourceName, metaV1.GetOptions{}); kerrors.IsNotFound(err) {
			// then resource with the given GVR is not found
			continue
		}
		if err != nil {
			return errors.Wrapf(err, "cannot get object: GVR: %s, Name: %s", gvr.String(), o.resourceName)
		}
		// having read the object, store its YAML manifest into a file for editing
		if o.originalJSon, err = obj.MarshalJSON(); err != nil {
			return errors.Wrap(err, "cannot marshal object into JSON")
		}
		buff, err := yaml.JSONToYAML(o.originalJSon)
		if err != nil {
			return errors.Wrap(err, "cannot convert object JSON to YAML")
		}
		if _, err = f.Write(buff); err != nil {
			return errors.Wrapf(err, "cannot write marshaled YAML to file: %s", f.Name())
		}
		if err = f.Sync(); err != nil {
			return errors.Wrapf(err, "cannot sync file: %s", f.Name())
		}
		// finally, store the GVK
		o.gvk, err = o.restMapper.KindFor(gvr)
		return errors.Wrapf(err, "cannot get GVK for GVR: %s", gvr.String())
	}
	return errors.Errorf("resource %s %q not found", o.resource, o.resourceName)
}

func (o *EditStatusOptions) editResource(f *os.File) error {
	cmd := exec.Command(o.resourceEditor, f.Name())
	cmd.Stdin = o.In
	cmd.Stdout = o.Out
	cmd.Stderr = o.ErrOut
	return errors.Wrapf(cmd.Run(), "cannot edit resource using editor: %q", o.resourceEditor)
}

func (o *EditStatusOptions) writeResourceStatus(f *os.File) error {
	restMapping, err := o.restMapper.RESTMapping(o.gvk.GroupKind(), o.gvk.Version)
	if err != nil {
		return errors.Wrapf(err, "cannot get REST mapping for GVK: %s", o.gvk.String())
	}

	editedYaml, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return errors.Wrapf(err, "cannot read edited object from file: %s", f.Name())
	}
	editedJSon, err := yaml.YAMLToJSON(editedYaml)
	if err != nil {
		return errors.Wrapf(err, "cannot convert edited object's YAML to JSON from file: %s", f.Name())
	}
	patch, err := jsonPatch.CreateMergePatch(o.originalJSon, editedJSon)
	if err != nil {
		return errors.Wrap(err, "cannot prepare merge patch")
	}

	restClient, err := apiutil.RESTClientForGVK(o.gvk, true, o.restConfig, serializer.CodecFactory{})
	if err != nil {
		return errors.Wrapf(err, "cannot get REST client for GVK: %s", o.gvk.String())
	}
	_, err = restClient.Patch(types.MergePatchType).
		NamespaceIfScoped(o.namespace, restMapping.Scope.Name() == meta.RESTScopeNameNamespace).
		Resource(restMapping.Resource.Resource).
		Name(o.resourceName).
		SubResource("status").
		VersionedParams(&metaV1.PatchOptions{}, metaV1.ParameterCodec).
		Body(patch).
		DoRaw(context.TODO())
	return errors.Wrapf(err, "cannot merge patch object: GVK: %s, Name: %s", o.gvk.String(), o.resourceName)
}
