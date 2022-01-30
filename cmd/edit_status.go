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
	"go.uber.org/multierr"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	delimEditorNames  = ":"
	defaultEditorName = "vi"
	defaultNamespaced = true
	patternTmpFile    = "kubectl-edit-status-"
	flagNamespaced    = "namespaced"

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
	cmd         *cobra.Command

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

	patchType types.PatchType
	patchBody []byte

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
	o.cmd = &cobra.Command{
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

	o.cmd.Flags().BoolVar(&o.namespaced, flagNamespaced, defaultNamespaced, "set to false for cluster-scoped resources")
	o.cmd.Flags().StringVarP(&o.resourceEditor, "editor", "e",
		fmt.Sprintf("${%s}%s${%s}%s%s",
			envKubeEditor, delimEditorNames, envEditor, delimEditorNames, defaultEditorName),
		fmt.Sprintf("editor to use. Either editor name in PATH or path to the editor executable. "+
			"If not specified, first value of %q and then value of %q environment variables are substituted and checked",
			envKubeEditor, envEditor))

	// add K8s generic client flags
	o.configFlags.AddFlags(o.cmd.Flags())
	return o.cmd
}

// Init ensures that all required arguments and flag values are provided and fills the EditStatusOptions receiver arg
func (o *EditStatusOptions) Init(_ *cobra.Command, args []string) error {
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
		return errors.Wrap(err, "cannot get discovery client")
	}

	o.restConfig, err = o.configFlags.ToRESTConfig()
	if err != nil {
		return errors.Wrap(err, "cannot get REST config")
	}

	o.dynamicClient, err = dynamic.NewForConfig(o.restConfig)
	if err != nil {
		return errors.Wrap(err, "cannot initialize a dynamic client")
	}

	o.restMapper, err = o.configFlags.ToRESTMapper()
	if err != nil {
		return errors.Wrap(err, "cannot get a REST mapper")
	}

	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return errors.Wrap(err, "cannot get configured namespace")
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
	if err := o.storeResource(); err != nil {
		return err
	}
	if err := o.editResource(); err != nil {
		return err
	}
	return o.patchResourceStatus()
}

func (o *EditStatusOptions) storeResourceWithGVR(gvr schema.GroupVersionResource) error {
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(o.discoveryClient)
	gvrs, err := restMapper.ResourcesFor(gvr)
	if err != nil {
		return errors.Wrap(err, "no GVRs found")
	}
	namespaced := defaultNamespaced
	for _, gvr := range gvrs {
		var obj *unstructured.Unstructured
		var ri dynamic.ResourceInterface = o.dynamicClient.Resource(gvr)
		namespaced, err = o.isNamespaced(gvr)
		if err != nil {
			return err
		}
		if namespaced {
			ri = ri.(dynamic.NamespaceableResourceInterface).Namespace(o.namespace)
		}
		if obj, err = ri.Get(context.TODO(),
			o.resourceName, metav1.GetOptions{}); kerrors.IsNotFound(err) {
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
		// finally, store the GVK
		o.gvk, err = o.restMapper.KindFor(gvr)
		return errors.Wrapf(err, "cannot get GVK for GVR: %s", gvr.String())
	}
	scope := "cluster-scoped"
	if namespaced {
		scope = fmt.Sprintf("namespaced (in %q)", o.namespace)
	}
	return errors.Wrapf(err, "%s resource %s %q with GVR %q not found",
		scope, o.resource, o.resourceName, gvr.String())
}

func (o *EditStatusOptions) storeResource() error {
	resourceParts := strings.Split(o.resource, ".")
	searchGVRs := []schema.GroupVersionResource{
		{
			Resource: resourceParts[0],
			Group:    strings.Join(resourceParts[1:], "."),
		},
	}
	// extend list of GVRs to be searched by any matching short names
	shortNameGVRs, err := o.resourcesForShortName(resourceParts[0])
	if err != nil {
		return err
	}
	// short-name matching resources are considered with lower priority
	searchGVRs = append(searchGVRs, shortNameGVRs...)
	var aggregatedErr error
	for _, gvr := range searchGVRs {
		err := o.storeResourceWithGVR(gvr)
		// as long as we have no resource match for
		// the partial resource specification or
		// no object with the given scope & name
		// we will continue searching.
		t := &meta.NoResourceMatchError{}
		if errors.As(err, &t) || kerrors.IsNotFound(err) {
			aggregatedErr = multierr.Append(aggregatedErr, err)
			continue
		}
		if err != nil {
			return err
		}
		return nil
	}
	return errors.Wrap(aggregatedErr,
		"cannot find any GVRs for the partial specification, or no objects have been found")
}

func (o *EditStatusOptions) resourcesForShortName(shortName string) ([]schema.GroupVersionResource, error) {
	var result []schema.GroupVersionResource
	_, arrResourceList, err := o.discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return nil, errors.Wrapf(err, "cannot discover all server resources to search for the short name: %s", shortName)
	}
	for _, resourceList := range arrResourceList {
		if resourceList == nil {
			continue
		}
		for _, r := range resourceList.APIResources {
			for _, sn := range r.ShortNames {
				if sn == shortName {
					result = append(result, schema.GroupVersionResource{
						Group:    r.Group,
						Version:  r.Version,
						Resource: r.Name,
					})
				}
			}
		}
	}
	return result, nil
}

// isNamespaced returns true if the resource is namespaced.
// if "--namespaced" is not explicitly set, then we try to infer
// the resource's scope.
func (o *EditStatusOptions) isNamespaced(gvr schema.GroupVersionResource) (bool, error) {
	if o.cmd.Flags().Changed(flagNamespaced) {
		return o.namespaced, nil
	}
	// try to infer resource scope from CRDs
	gv := schema.GroupVersion{
		Group:   gvr.Group,
		Version: gvr.Version,
	}.String()
	resourceList, err := o.discoveryClient.ServerResourcesForGroupVersion(gv)
	if err != nil {
		return false, errors.Wrapf(err, "cannot discover server resources for GV: %q", gv)
	}
	for _, r := range resourceList.APIResources {
		if r.Name == gvr.Resource {
			return r.Namespaced, nil
		}
	}
	return defaultNamespaced, nil
}

func (o *EditStatusOptions) editResource() (err error) {
	f, err := ioutil.TempFile(os.TempDir(), patternTmpFile)
	if err != nil {
		return errors.Wrap(err, "cannot create temp file")
	}

	defer func() {
		err = multierr.Append(err, errors.Wrapf(os.Remove(f.Name()),
			"cannot remove temporary file: %s", f.Name()))
	}()

	// first store the resource into the temp file
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
	// now open an editor to edit it
	if err := o.execEditorCommand(f.Name()); err != nil {
		return err
	}
	// calculate and store JSON merge patch document from edited resource manifest
	return o.calculateMergePatch(f)
}

func (o *EditStatusOptions) execEditorCommand(tmpFilePath string) error {
	parts := strings.Fields(o.resourceEditor)
	execName := ""
	var args []string
	switch len(parts) {
	case 0:
		return errors.Errorf("invalid editor specification: %q", o.resourceEditor)

	default:
		args = parts[1:]
		fallthrough

	case 1:
		execName = parts[0]
		args = append(args, tmpFilePath)
	}
	cmd := exec.Command(execName, args...)
	cmd.Stdin = o.In
	cmd.Stdout = o.Out
	cmd.Stderr = o.ErrOut
	return errors.Wrapf(cmd.Run(), "cannot edit resource using editor. Command-line: %q", cmd.String())
}

func (o *EditStatusOptions) calculateMergePatch(f *os.File) error {
	editedYaml, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return errors.Wrapf(err, "cannot read edited object from file: %s", f.Name())
	}
	editedJSon, err := yaml.YAMLToJSON(editedYaml)
	if err != nil {
		return errors.Wrapf(err, "cannot convert edited object's YAML to JSON from file: %s", f.Name())
	}
	o.patchType = types.MergePatchType
	o.patchBody, err = jsonPatch.CreateMergePatch(o.originalJSon, editedJSon)
	return errors.Wrap(err, "cannot prepare merge patch")
}

func (o *EditStatusOptions) patchResourceStatus() error {
	restMapping, err := o.restMapper.RESTMapping(o.gvk.GroupKind(), o.gvk.Version)
	if err != nil {
		return errors.Wrapf(err, "cannot get REST mapping for GVK: %s", o.gvk.String())
	}

	restClient, err := apiutil.RESTClientForGVK(o.gvk, true, o.restConfig, serializer.CodecFactory{})
	if err != nil {
		return errors.Wrapf(err, "cannot get REST client for GVK: %s", o.gvk.String())
	}
	_, err = restClient.Patch(o.patchType).
		NamespaceIfScoped(o.namespace, restMapping.Scope.Name() == meta.RESTScopeNameNamespace).
		Resource(restMapping.Resource.Resource).
		Name(o.resourceName).
		SubResource("status").
		VersionedParams(&metav1.PatchOptions{}, metav1.ParameterCodec).
		Body(o.patchBody).
		DoRaw(context.TODO())
	return errors.Wrapf(err, "cannot merge patch object: GVK: %s, Name: %s", o.gvk.String(), o.resourceName)
}
