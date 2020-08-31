# kubectl edit-status
A kubectl plugin for editing /status subresource.

This is a [plugin](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) for `kubectl`. 
It aims to provide an `edit-status` command for `kubectl` that allows editing of 
`/status` [subresources](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#subresources).
With the current plugin mechanism of `kubectl`, it's currently not possible to extend an existing command with a subcommand, 
so this plugin adds a new command (`edit-status`) to `kubectl`.

## Installation
You can use [krew](https://krew.sigs.k8s.io/) to install `edit-status`. If you have not installed `krew` yet, which itself is a `kubectl` plugin, 
you may do so by following `krew`'s [installation instructions](https://krew.sigs.k8s.io/docs/user-guide/setup/install/). 
`edit-status` is not yet available through `krew`'s [default index](https://github.com/kubernetes-sigs/krew-index), 
and this repository also acts as a [custom plugin index](https://krew.sigs.k8s.io/docs/developer-guide/custom-indexes/). 
In order to install `edit-status`:
- Please make sure that your `krew` plugin supports custom plugin indexes (version [`0.4.0`](https://github.com/kubernetes-sigs/krew/releases/tag/v0.4.0) or higher).
- Add this repository as a custom plugin index that indexes just one plugin as of right now: <br />
`kubectl krew index add edit-status https://github.com/ulucinar/kubectl-edit-status`
- Update plugin index: <br />
`kubectl krew update`
- Install `edit-status` plugin: <br />
`kubectl krew install edit-status/edit-status`

After installation, if you have `krew`'s plugin `bin` directory on your path, `kubectl plugin list` should list `edit-status` plugin as `kubectl-edit_status`. 
You may run `kubectl edit-status --help` to see command-line options:

```
$ kubectl edit-status --help
Edit /status subresource

Usage:
  kubectl edit-status [resource] [resource-name] [flags]

Examples:

	# edit the status field of the MyResource CR named "test", which uses status subresource 
	kubectl edit-status myresource test


Flags:
      --as string                      Username to impersonate for the operation
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --cache-dir string               Default HTTP cache directory (default "/Users/alper/.kube/http-cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --context string                 The name of the kubeconfig context to use
  -e, --editor string                  editor to use. Either editor name in PATH or path to the editor executable. If not specified, first value of "KUBE_EDITOR" and then value of "EDITOR" environment variables are substituted and checked (default "${KUBE_EDITOR}:${EDITOR}:vi")
  -h, --help                           help for kubectl
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
      --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
  -n, --namespace string               If present, the namespace scope for this CLI request
      --namespaced                     set to false for cluster-scoped resources (default true)
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
  -s, --server string                  The address and port of the Kubernetes API server
      --tls-server-name string         Server name to use for server certificate validation. If it is not provided, the hostname used to contact the server is used
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use
```

## License

Apache 2.0. See [LICENSE](./LICENSE).
